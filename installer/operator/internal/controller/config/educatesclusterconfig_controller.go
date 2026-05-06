/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

// singletonRequest is the only enqueue target for this controller —
// EducatesClusterConfig is a singleton named "cluster", so any change
// to a referenced resource maps to that one Reconcile request.
var singletonRequest = []reconcile.Request{
	{NamespacedName: types.NamespacedName{Name: "cluster"}},
}

// mapToSingleton enqueues the singleton EducatesClusterConfig regardless
// of which referenced resource changed. The reconciler is idempotent
// and re-runs full validation each pass, so over-enqueuing is cheap.
// Filtering by name ("only enqueue if this Secret is referenced from
// spec.inline") would require reading spec at predicate time and saves
// little in a singleton model.
func mapToSingleton(_ context.Context, _ client.Object) []reconcile.Request {
	return singletonRequest
}

// finalizerName is set on EducatesClusterConfig so the operator gets a
// chance to clean up before the resource is removed. Inline mode has
// nothing to clean up; Phase 2 Managed mode reuses the same name.
const finalizerName = "educatesclusterconfig.config.educates.dev/finalizer"

// Condition types published by Phase 1. Managed-mode condition types
// (IngressReady, CertificatesReady, DNSReady, PolicyEnforcementReady,
// InfrastructureConfigured) are added in later phases alongside their
// producing reconcilers.
const (
	conditionReady               = "Ready"
	conditionValidationSucceeded = "ValidationSucceeded"
)

// EducatesClusterConfigReconciler reconciles a EducatesClusterConfig object.
type EducatesClusterConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// OperatorNamespace is where user-supplied Secrets (TLS, CA, image-
	// pull) referenced from spec.inline are expected to live. Sourced
	// from the OPERATOR_NAMESPACE env var (downward API).
	OperatorNamespace string
}

// +kubebuilder:rbac:groups=config.educates.dev,resources=educatesclusterconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=config.educates.dev,resources=educatesclusterconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=config.educates.dev,resources=educatesclusterconfigs/finalizers,verbs=update

// Inline-mode validation reads user-supplied references in the operator
// namespace (Secrets) plus cluster-scoped objects (ClusterIssuers,
// IngressClasses). All read-only.
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=cert-manager.io,resources=clusterissuers,verbs=get;list;watch
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingressclasses,verbs=get;list;watch

// Reconcile drives the EducatesClusterConfig singleton through its
// lifecycle. Phase 1 implements Inline mode (validate referenced
// resources and publish them in status); Managed mode is a no-op stub
// until Phase 2 wires Helm-SDK chart installs.
func (r *EducatesClusterConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling EducatesClusterConfig", "name", req.Name)

	obj := &configv1alpha1.EducatesClusterConfig{}
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Deletion path: drain finalizer.
	if !obj.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(obj, finalizerName) {
			// Phase 1 Inline cleanup is a no-op; Phase 2 Managed mode will
			// uninstall charts here in reverse install order.
			controllerutil.RemoveFinalizer(obj, finalizerName)
			if err := r.Update(ctx, obj); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Set the finalizer on first sight; requeue so the next pass sees a
	// stable resource version with status writes.
	if !controllerutil.ContainsFinalizer(obj, finalizerName) {
		controllerutil.AddFinalizer(obj, finalizerName)
		if err := r.Update(ctx, obj); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Managed-mode handling lands in Phase 2.
	if obj.Spec.Mode != configv1alpha1.ClusterConfigModeInline {
		return ctrl.Result{}, nil
	}

	// CEL guarantees spec.inline is set when mode is Inline; guard
	// defensively in case CEL is bypassed (e.g., by a controller writing
	// against the API directly without admission).
	if obj.Spec.Inline == nil {
		r.markDegraded(obj, "spec.inline", "Inline mode requires spec.inline to be set")
		return ctrl.Result{}, r.Status().Update(ctx, obj)
	}

	statusIngress, err := r.validateInline(ctx, obj.Spec.Inline)
	if err != nil {
		var verr *validationError
		if errors.As(err, &verr) {
			r.markDegraded(obj, verr.Field, verr.Reason)
			return ctrl.Result{}, r.Status().Update(ctx, obj)
		}
		// API error (lookup failed, transient): surface for retry.
		return ctrl.Result{}, err
	}

	r.markReady(obj, statusIngress)
	return ctrl.Result{}, r.Status().Update(ctx, obj)
}

// markReady populates the inter-CR status contract and flips conditions
// to True. Called once Inline validation has succeeded.
func (r *EducatesClusterConfigReconciler) markReady(obj *configv1alpha1.EducatesClusterConfig, ingress *configv1alpha1.StatusIngress) {
	obj.Status.ObservedGeneration = obj.Generation
	obj.Status.Phase = configv1alpha1.ClusterConfigPhaseReady
	obj.Status.Mode = obj.Spec.Mode
	obj.Status.Ingress = ingress
	obj.Status.PolicyEnforcement = &configv1alpha1.StatusPolicyEnforcement{
		ClusterPolicyEngine:  obj.Spec.Inline.PolicyEnforcement.ClusterPolicyEngine,
		WorkshopPolicyEngine: obj.Spec.Inline.PolicyEnforcement.WorkshopPolicyEngine,
	}
	if obj.Spec.Inline.ImageRegistry != nil {
		obj.Status.ImageRegistry = obj.Spec.Inline.ImageRegistry.DeepCopy()
	} else {
		// Always populate so components see a single source of truth.
		obj.Status.ImageRegistry = &configv1alpha1.ImageRegistry{}
	}

	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionValidationSucceeded,
		Status:             metav1.ConditionTrue,
		Reason:             "InlineRefsValid",
		Message:            "All Inline-mode references validated",
		ObservedGeneration: obj.Generation,
	})
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "ValidationSucceeded",
		Message:            "EducatesClusterConfig is ready",
		ObservedGeneration: obj.Generation,
	})
}

// markDegraded flips conditions to False with a field-specific message
// without touching the published interface fields (status.ingress,
// status.policyEnforcement, status.imageRegistry) — components keep
// reading the last-known-good values until validation recovers, just
// as Ready: False signals.
func (r *EducatesClusterConfigReconciler) markDegraded(obj *configv1alpha1.EducatesClusterConfig, field, reason string) {
	obj.Status.ObservedGeneration = obj.Generation
	obj.Status.Phase = configv1alpha1.ClusterConfigPhaseDegraded
	obj.Status.Mode = obj.Spec.Mode

	msg := fmt.Sprintf("%s: %s", field, reason)
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionValidationSucceeded,
		Status:             metav1.ConditionFalse,
		Reason:             "InlineRefsInvalid",
		Message:            msg,
		ObservedGeneration: obj.Generation,
	})
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "ValidationFailed",
		Message:            msg,
		ObservedGeneration: obj.Generation,
	})
}

// SetupWithManager sets up the controller with the Manager.
//
// Watches:
//   - Secrets (cache-restricted to the operator namespace by main.go)
//   - IngressClasses (cluster-scoped)
//
// ClusterIssuer is intentionally NOT watched in Phase 1: the type is
// served by an out-of-tree CRD that may or may not be installed at
// startup, and an unstructured watch fails hard at cache-startup if
// the CRD is absent. Phase 2 vendors cert-manager Go types and adds
// the watch unconditionally (Managed mode always installs cert-manager
// when bundled). Inline-mode users referencing a ClusterIssuer can
// re-trigger validation by touching spec until then.
func (r *EducatesClusterConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1alpha1.EducatesClusterConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(mapToSingleton)).
		Watches(&networkingv1.IngressClass{}, handler.EnqueueRequestsFromMapFunc(mapToSingleton)).
		Named("config-educatesclusterconfig").
		Complete(r)
}
