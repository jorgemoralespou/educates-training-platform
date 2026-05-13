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

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
)

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

	// HelmClientFor returns a Helm client scoped to the given
	// namespace. Production wiring builds a REST-config-backed client
	// (main.go); reconciler tests inject a factory returning an
	// in-memory client so install/upgrade/status paths can be exercised
	// without an apiserver. Required for Managed mode; unused in
	// Inline mode.
	HelmClientFor func(namespace string) (*helm.Client, error)
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

// Managed-mode operations:
//   - Namespaces (create/patch for cluster-service installs).
//   - Secrets (write — copy CustomCA into cert-manager namespace).
//   - Deployments (watch — cert-manager readiness gate).
//   - cert-manager ClusterIssuers + Certificates (SSA + watch).
// Helm-managed resources (cert-manager's own ConfigMaps, Services,
// MutatingWebhookConfigurations, etc.) ride on the helm SDK's
// internal kube client and don't need explicit verbs here — but they
// will when Phase 6 removes the cluster-admin shortcut.
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=cert-manager.io,resources=clusterissuers,verbs=create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete

// Reconcile drives the EducatesClusterConfig singleton through its
// lifecycle. Phase 1 implements Inline mode (validate referenced
// resources and publish them in status); Managed mode is a no-op stub
// until Phase 2 wires Helm-SDK chart installs.
func (r *EducatesClusterConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	obj := &configv1alpha1.EducatesClusterConfig{}
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		// NotFound is the steady state when no EducatesClusterConfig
		// exists: watched resources (Secrets, IngressClasses, etc.)
		// still enqueue the singleton on every event, so this branch
		// fires often. Log nothing — controller-runtime's debug-level
		// "Reconciling" trace is enough for observability.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Per-reconcile entry log lives at V(1): controller-runtime emits
	// its own reconcileID trace at the same level, so an INFO log here
	// just duplicates it and floods the console during cert-manager
	// bootstrap (every Deployment/Certificate/Secret event enqueues a
	// reconcile). The high-signal events — Ready transitions, webhook-
	// not-ready, condition flips — are still logged at INFO from
	// updateStatusWithTransitionLog and the dedicated handlers below.
	log.V(1).Info("Reconciling EducatesClusterConfig")

	// Deletion path: drain finalizer. Managed mode tears down its
	// installed cluster services in reverse install order so cert-manager
	// is still alive to process the Certificate/ClusterIssuer deletions
	// before the chart itself is uninstalled. Inline mode has nothing to
	// undo. Cleanup is idempotent — retried reconciles after partial
	// failure re-attempt only what's still present.
	if !obj.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(obj, finalizerName) {
			if obj.Spec.Mode == configv1alpha1.ClusterConfigModeManaged {
				if err := r.cleanupManaged(ctx, obj); err != nil {
					log.Error(err, "Managed-mode cleanup failed; reconcile will retry")
					return ctrl.Result{}, err
				}
			}
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

	// Managed mode delegates to the Phase 2 install pipeline; Inline
	// mode stays in the Phase 1 validator.
	if obj.Spec.Mode == configv1alpha1.ClusterConfigModeManaged {
		return r.reconcileManaged(ctx, obj)
	}

	// CEL guarantees spec.inline is set when mode is Inline; guard
	// defensively in case CEL is bypassed (e.g., by a controller writing
	// against the API directly without admission).
	if obj.Spec.Inline == nil {
		r.markDegraded(obj, "spec.inline", "Inline mode requires spec.inline to be set")
		return ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, log, obj)
	}

	statusIngress, err := r.validateInline(ctx, obj.Spec.Inline)
	if err != nil {
		var verr *validationError
		if errors.As(err, &verr) {
			r.markDegraded(obj, verr.Field, verr.Reason)
			return ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, log, obj)
		}
		// API error (lookup failed, transient): surface for retry.
		return ctrl.Result{}, err
	}

	r.markReady(obj, statusIngress)
	return ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, log, obj)
}

// readyConditionIsTrue reports whether the Ready condition is currently
// True. Used to detect transitions in either direction
// (False/Unknown↔True) for logging purposes.
func readyConditionIsTrue(obj *configv1alpha1.EducatesClusterConfig) bool {
	c := meta.FindStatusCondition(obj.Status.Conditions, conditionReady)
	return c != nil && c.Status == metav1.ConditionTrue
}

// updateStatusWithTransitionLog persists status changes and emits an
// INFO log line on either Ready transition: False/Unknown→True (the
// "we just became healthy" signal) or True→False (the "something
// degraded" signal). Steady-state re-reconciles that don't change
// Ready stay silent so operators don't have to filter watch-noise out
// of their console.
//
// priorReady is sampled from a LIVE Get inside the retry block, not
// from the cached obj passed in at the top of Reconcile. The cache can
// lag etcd by a few hundred ms after our own Status().Update lands,
// during which a separate watch event triggers another Reconcile whose
// cached read still shows the old Ready=False. Sampling priorReady
// from cache there would log "Ready=True" twice in a row for the same
// transition. Live read avoids that.
//
// Conflict handling: controller-runtime's cache-backed client can
// hand back a stale resourceVersion. Status().Update against that
// stale version collides with etcd's newer revision. RetryOnConflict
// re-Gets the latest, replays our intended status onto it, and
// retries — IsConflict is the only retryable error class, so transient
// API timeouts surface as-is.
//
// All Managed/Inline status-write sites funnel through here so any new
// branch added later inherits both behaviours.
func (r *EducatesClusterConfigReconciler) updateStatusWithTransitionLog(ctx context.Context, log logr.Logger, obj *configv1alpha1.EducatesClusterConfig) error {
	intendedStatus := obj.Status
	key := client.ObjectKeyFromObject(obj)
	var priorReady bool
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &configv1alpha1.EducatesClusterConfig{}
		if err := r.Get(ctx, key, latest); err != nil {
			return err
		}
		priorReady = readyConditionIsTrue(latest)
		latest.Status = intendedStatus
		return r.Status().Update(ctx, latest)
	}); err != nil {
		return err
	}
	nowReady := readyConditionIsTrue(obj)
	switch {
	case !priorReady && nowReady:
		log.Info("EducatesClusterConfig reconciliation complete; Ready=True",
			"mode", obj.Spec.Mode,
			"phase", obj.Status.Phase,
			"certManagerVersion", obj.Status.BundledChartVersions["cert-manager"])
	case priorReady && !nowReady:
		cond := meta.FindStatusCondition(obj.Status.Conditions, conditionReady)
		reason, message := "", ""
		if cond != nil {
			reason = cond.Reason
			message = cond.Message
		}
		log.Info("EducatesClusterConfig degraded; Ready was True, now False",
			"phase", obj.Status.Phase,
			"reason", reason,
			"message", message)
	}
	return nil
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
//   - Secrets (cache-restricted to the operator namespace by main.go).
//   - IngressClasses (cluster-scoped).
//   - ClusterIssuers + Certificates (cert-manager.io/v1, registered as
//     unstructured so the operator pod starts on a vanilla cluster
//     where cert-manager hasn't been installed yet — see below).
//   - Deployments (cluster-wide; cert-manager-namespace events drive
//     the readiness gate).
//
// cert-manager.io kinds use the unstructured-watch form. Typed
// watches (`Watches(&cmv1.ClusterIssuer{}, ...)`) would require the
// GVK to be resolvable at cache startup, which means cert-manager
// CRDs would have to be applied to the cluster *before* the operator
// pod runs — even for Managed-mode users for whom the operator
// itself is supposed to install cert-manager. Unstructured watches
// start successfully whether or not the CRD exists; events flow once
// the CRD lands. Code paths that Get / Create / Update / SSA-patch
// these kinds still use the typed `cmv1.*` types — those calls only
// fire after cert-manager is installed (`ensureCertManagerReady`),
// at which point the CRDs are present and typed access works
// normally. See decisions.md (2026-05-06 entry, 2026-05-13 reversal
// amendment).
func (r *EducatesClusterConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	clusterIssuerWatch := &unstructured.Unstructured{}
	clusterIssuerWatch.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   cmv1.SchemeGroupVersion.Group,
		Version: cmv1.SchemeGroupVersion.Version,
		Kind:    "ClusterIssuer",
	})
	certificateWatch := &unstructured.Unstructured{}
	certificateWatch.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   cmv1.SchemeGroupVersion.Group,
		Version: cmv1.SchemeGroupVersion.Version,
		Kind:    "Certificate",
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&configv1alpha1.EducatesClusterConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(r.mapSecretToSingleton)).
		Watches(&networkingv1.IngressClass{}, handler.EnqueueRequestsFromMapFunc(r.mapIngressClassToSingleton)).
		Watches(clusterIssuerWatch, handler.EnqueueRequestsFromMapFunc(r.mapClusterIssuerToSingleton)).
		Watches(certificateWatch, handler.EnqueueRequestsFromMapFunc(r.mapCertificateToSingleton)).
		Watches(&appsv1.Deployment{}, handler.EnqueueRequestsFromMapFunc(r.mapDeploymentToSingleton)).
		Named("config-educatesclusterconfig").
		Complete(r)
}
