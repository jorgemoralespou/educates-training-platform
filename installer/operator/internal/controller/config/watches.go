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

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

// Per-kind mapping functions that filter watch events at the source.
// The previous design enqueued the singleton on *any* event from any
// watched kind; cert-manager bootstrap could fire ~20 reconciles in
// two minutes from cluster-wide Deployment/Certificate/Secret churn,
// most of which had nothing to do with Educates. These mappers drop
// events the reconciler can't act on, so Reconcile fires only when
// something actually relevant changed.
//
// All mappers share a common shape:
//
//  1. Operator-owned resources (wildcard tls Secret, copied CustomCA
//     Secret, wildcard ClusterIssuer + Certificate, cert-manager
//     namespace's Deployments) always enqueue regardless of CR state.
//     We need to detect drift on resources we created even if the CR
//     is mid-deletion.
//  2. Otherwise, look up the singleton CR. If it doesn't exist, drop
//     the event — there's nothing to reconcile against. The CR's
//     own creation event will wake the reconciler when it appears.
//  3. With the CR in hand, consult mode-specific spec fields to
//     decide whether the event names a resource we care about.
//
// Filters by kind:
//   - Secrets: operator-owned + spec.inline/ingress references +
//     spec.imageRegistry.pullSecrets.
//   - IngressClass: spec.{inline.ingress|ingress}.ingressClassName.
//   - ClusterIssuer: operator-owned + spec.inline.ingress.clusterIssuerRef.
//   - Certificate: operator-owned (wildcard) only.
//   - Deployment: operator-managed namespaces (cert-manager, Contour,
//     external-dns, Kyverno).
//
// Each mapper takes the controller-runtime client context and the
// changed object, and returns either the singleton-enqueue list or
// nil. Returning nil drops the event before it reaches the work queue.

// singletonRequest is the only enqueue target for this controller —
// EducatesClusterConfig is a singleton named "cluster", so any
// relevant event maps to that one Reconcile request.
var singletonRequest = []reconcile.Request{
	{NamespacedName: types.NamespacedName{Name: "cluster"}},
}

// getSingleton fetches the EducatesClusterConfig from the cache.
// Returns (nil, false) when the CR doesn't exist yet — mappers use
// that to drop events when there's no work to do.
func (r *EducatesClusterConfigReconciler) getSingleton(ctx context.Context) (*configv1alpha1.EducatesClusterConfig, bool) {
	cr := &configv1alpha1.EducatesClusterConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: "cluster"}, cr); err != nil {
		return nil, false
	}
	return cr, true
}

// mapSecretToSingleton fires Reconcile only for Secrets the operator
// has a reason to react to: ones it owns, or ones referenced from
// spec. Cluster-wide Secret churn (most of which is unrelated) gets
// dropped here.
func (r *EducatesClusterConfigReconciler) mapSecretToSingleton(ctx context.Context, obj client.Object) []reconcile.Request {
	name, ns := obj.GetName(), obj.GetNamespace()

	// Operator-owned: wildcard tls Secret (operator ns) and the
	// CustomCA copy (cert-manager ns). Always enqueue.
	if ns == r.OperatorNamespace && name == wildcardTLSSecretName {
		return singletonRequest
	}
	if ns == certManagerNamespace && name == customCASecretName {
		return singletonRequest
	}

	cr, ok := r.getSingleton(ctx)
	if !ok {
		return nil
	}

	// matchesOpNamespaceRef holds for refs that are operator-namespace-
	// scoped by design (everything except CustomCA, which can be
	// cross-namespace via CASecretReference).
	matchesOpNamespaceRef := func(refName string) bool {
		return ns == r.OperatorNamespace && name == refName
	}

	switch cr.Spec.Mode {
	case configv1alpha1.ClusterConfigModeInline:
		if cr.Spec.Inline == nil {
			return nil
		}
		if ref := cr.Spec.Inline.Ingress.WildcardCertificateSecretRef; ref != nil && matchesOpNamespaceRef(ref.Name) {
			return singletonRequest
		}
		if ref := cr.Spec.Inline.Ingress.CACertificateSecretRef; ref != nil {
			refNS := ref.Namespace
			if refNS == "" {
				refNS = r.OperatorNamespace
			}
			if ns == refNS && name == ref.Name {
				return singletonRequest
			}
		}
		if cr.Spec.Inline.ImageRegistry != nil {
			for _, ref := range cr.Spec.Inline.ImageRegistry.PullSecrets {
				if matchesOpNamespaceRef(ref.Name) {
					return singletonRequest
				}
			}
		}
	case configv1alpha1.ClusterConfigModeManaged:
		if bcm := cr.Spec.Ingress; bcm != nil &&
			bcm.Certificates.BundledCertManager != nil &&
			bcm.Certificates.BundledCertManager.CustomCA != nil {
			ref := bcm.Certificates.BundledCertManager.CustomCA.CACertificateRef
			refNS := ref.Namespace
			if refNS == "" {
				refNS = r.OperatorNamespace
			}
			// CASecretReference allows cross-namespace; compare both
			// namespace and name. Without this, watches on a CA Secret
			// in (e.g.) educates-secrets never enqueued a reconcile and
			// rotations went unnoticed.
			if ns == refNS && name == ref.Name {
				return singletonRequest
			}
		}
		if cr.Spec.ImageRegistry != nil {
			for _, ref := range cr.Spec.ImageRegistry.PullSecrets {
				if matchesOpNamespaceRef(ref.Name) {
					return singletonRequest
				}
			}
		}
	}
	return nil
}

// mapIngressClassToSingleton fires only for the IngressClass named
// from spec. Cluster-wide IngressClass churn is otherwise dropped.
func (r *EducatesClusterConfigReconciler) mapIngressClassToSingleton(ctx context.Context, obj client.Object) []reconcile.Request {
	cr, ok := r.getSingleton(ctx)
	if !ok {
		return nil
	}
	switch cr.Spec.Mode {
	case configv1alpha1.ClusterConfigModeInline:
		if cr.Spec.Inline != nil && obj.GetName() == cr.Spec.Inline.Ingress.IngressClassName {
			return singletonRequest
		}
	case configv1alpha1.ClusterConfigModeManaged:
		if cr.Spec.Ingress != nil && obj.GetName() == cr.Spec.Ingress.IngressClassName {
			return singletonRequest
		}
	}
	return nil
}

// mapClusterIssuerToSingleton fires for the operator-owned wildcard
// ClusterIssuer and (in Inline mode) for a user-referenced
// ClusterIssuer. Other cluster-scoped issuers are dropped.
func (r *EducatesClusterConfigReconciler) mapClusterIssuerToSingleton(ctx context.Context, obj client.Object) []reconcile.Request {
	if obj.GetName() == wildcardClusterIssuer {
		return singletonRequest
	}
	cr, ok := r.getSingleton(ctx)
	if !ok {
		return nil
	}
	if cr.Spec.Mode == configv1alpha1.ClusterConfigModeInline &&
		cr.Spec.Inline != nil &&
		cr.Spec.Inline.Ingress.ClusterIssuerRef != nil &&
		obj.GetName() == cr.Spec.Inline.Ingress.ClusterIssuerRef.Name {
		return singletonRequest
	}
	return nil
}

// mapCertificateToSingleton fires only for the operator-owned
// wildcard Certificate. cert-manager creates and updates many other
// Certificates in production clusters (one per ingress, per workshop
// session, etc.); none of them are our concern.
func (r *EducatesClusterConfigReconciler) mapCertificateToSingleton(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetName() == wildcardCertificate && obj.GetNamespace() == r.OperatorNamespace {
		return singletonRequest
	}
	return nil
}

// mapPlatformCRToSingleton fires for the three platform component
// singletons (SecretsManager, LookupService, SessionManager). The
// Managed-mode finalizer refuses to drain cluster services while any
// of them exist, so their deletion events are what unblock a pending
// EducatesClusterConfig teardown. CEL enforces the singleton name;
// anything else is dropped defensively.
func (r *EducatesClusterConfigReconciler) mapPlatformCRToSingleton(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetName() == "cluster" {
		return singletonRequest
	}
	return nil
}

// mapDeploymentToSingleton fires only for Deployments in namespaces
// the operator manages cluster-services in. Each new cluster service
// adds its namespace here so its readiness signals reach the
// reconciler.
func (r *EducatesClusterConfigReconciler) mapDeploymentToSingleton(_ context.Context, obj client.Object) []reconcile.Request {
	switch obj.GetNamespace() {
	case certManagerNamespace, contourNamespace, externalDNSNamespace, kyvernoNamespace:
		return singletonRequest
	}
	return nil
}
