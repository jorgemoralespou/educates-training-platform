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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// Managed-mode condition types. Phase 2 Session 2 introduces
// CertificatesReady (cert-manager + ClusterIssuer + wildcard
// Certificate). Sibling conditions (IngressReady, DNSReady,
// PolicyEnforcementReady, InfrastructureConfigured) land alongside
// their producing reconcilers in Phase 3.
const conditionCertificatesReady = "CertificatesReady"

// Cluster-service install constants. Cert-manager is conventionally
// installed in its own namespace; the operator does not give users a
// knob to relocate it because all known upstream tooling (kubectl
// plugins, dashboards, RBAC defaults) assumes the canonical name.
const (
	certManagerNamespace   = "cert-manager"
	certManagerReleaseName = "cert-manager"
)

// reconcileManaged drives Phase 2 Managed-mode reconciliation. Phase 2
// Session 2 (this commit) installs cert-manager from the vendored
// chart and records the chart version in status; webhook readiness,
// ClusterIssuer/Certificate creation, and a True CertificatesReady
// condition land in Session 2 commit 2. Other cluster services (Contour,
// external-dns, Kyverno) land in Phase 3.
func (r *EducatesClusterConfigReconciler) reconcileManaged(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if err := r.validateManaged(ctx, obj); err != nil {
		var verr *validationError
		if errors.As(err, &verr) {
			r.markDegraded(obj, verr.Field, verr.Reason)
			return ctrl.Result{}, r.Status().Update(ctx, obj)
		}
		return ctrl.Result{}, err
	}

	// cert-manager install. Other cluster services follow the same
	// shape in Phase 3.
	if err := r.reconcileCertManager(ctx, obj); err != nil {
		log.Error(err, "cert-manager reconcile failed")
		// Surface in status as a CertificatesReady=False condition with
		// the error message; let controller-runtime retry on the
		// returned error.
		r.markCertificatesProgressing(obj, "InstallFailed", err.Error())
		_ = r.Status().Update(ctx, obj)
		return ctrl.Result{}, err
	}

	// Until Session 2 commit 2 lands webhook readiness +
	// ClusterIssuer/Certificate, CertificatesReady stays False with a
	// progressing reason. status.phase reflects this as Progressing.
	r.markCertificatesProgressing(obj, "Installing", "cert-manager chart installed; awaiting webhook readiness and issuer wiring")
	r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseInstalling)
	return ctrl.Result{}, r.Status().Update(ctx, obj)
}

// reconcileCertManager ensures the cert-manager release exists,
// installing from the vendored tarball on first sight. Upgrades on
// chart-version drift are handled here too (a vendored bump produces
// a different chart.Metadata.Version, the Status path notices, and
// Upgrade runs). Resource-level readiness checks (Deployment +
// webhook discovery) land in commit 2 of this session.
func (r *EducatesClusterConfigReconciler) reconcileCertManager(ctx context.Context, owner *configv1alpha1.EducatesClusterConfig) error {
	chrt, err := vendoredcharts.CertManager()
	if err != nil {
		return fmt.Errorf("load embedded cert-manager chart: %w", err)
	}

	if err := r.ensureNamespace(ctx, certManagerNamespace, nil, owner); err != nil {
		return err
	}

	hc, err := r.HelmClientFor(certManagerNamespace)
	if err != nil {
		return fmt.Errorf("build helm client for %q: %w", certManagerNamespace, err)
	}

	vals := renderCertManagerValues(owner)

	rel, err := hc.Status(certManagerReleaseName)
	switch {
	case errors.Is(err, helm.ErrReleaseNotFound):
		if _, err := hc.Install(ctx, certManagerReleaseName, chrt, vals); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		// Release exists. Upgrade only if the embedded chart version has
		// drifted from what was last installed; otherwise leave the
		// release alone to avoid spurious rollouts.
		if rel.Chart != nil && rel.Chart.Metadata != nil && rel.Chart.Metadata.Version != chrt.Metadata.Version {
			if _, err := hc.Upgrade(ctx, certManagerReleaseName, chrt, vals); err != nil {
				return err
			}
		}
	}

	if obj := owner; obj != nil {
		if obj.Status.BundledChartVersions == nil {
			obj.Status.BundledChartVersions = map[string]string{}
		}
		obj.Status.BundledChartVersions["cert-manager"] = vendoredcharts.CertManagerVersion
	}
	return nil
}

// renderCertManagerValues builds the values map passed to the
// cert-manager chart. Phase 2 Session 2 commit 1 uses chart defaults;
// image-registry-prefix rewriting and operational overrides land
// alongside the rest of the Managed-mode CR fields in later commits.
// Kept as a standalone function so values-shape changes don't ripple
// through reconcile control flow.
func renderCertManagerValues(_ *configv1alpha1.EducatesClusterConfig) map[string]any {
	return map[string]any{}
}

// validateManaged runs the Phase 2 Managed-mode checks. The CRD's CEL
// rules already enforce field-presence and mutual-exclusion at admission
// time; this validator covers cross-resource concerns (referenced
// Secrets exist with the right keys) and the not-yet-supported feature
// matrix.
//
// Session 2 commit 1 supports the minimal path that the phase's "done
// when" criteria require: BundledCertManager + CustomCA, with
// BundledContour ingress. Other providers/issuer types return explicit
// validation errors with a "not yet supported in v1alpha1" message
// rather than silently no-oping.
func (r *EducatesClusterConfigReconciler) validateManaged(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) error {
	if obj.Spec.Ingress == nil {
		return &validationError{
			Field:  "spec.ingress",
			Reason: "Managed mode requires spec.ingress",
		}
	}

	switch obj.Spec.Ingress.Controller.Provider {
	case configv1alpha1.IngressControllerProviderBundledContour:
		// supported; install lands in Phase 3.
	default:
		return &validationError{
			Field:  "spec.ingress.controller.provider",
			Reason: fmt.Sprintf("provider %q is not yet supported in v1alpha1", obj.Spec.Ingress.Controller.Provider),
		}
	}

	certs := obj.Spec.Ingress.Certificates
	switch certs.Provider {
	case configv1alpha1.CertificatesProviderBundledCertManager:
		if certs.BundledCertManager == nil {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager",
				Reason: "required when certificates.provider is BundledCertManager",
			}
		}
		switch certs.BundledCertManager.IssuerType {
		case configv1alpha1.IssuerTypeCustomCA:
			if certs.BundledCertManager.CustomCA == nil {
				return &validationError{
					Field:  "spec.ingress.certificates.bundledCertManager.customCA",
					Reason: "required when issuerType is CustomCA",
				}
			}
			if err := r.checkCustomCASecret(ctx, certs.BundledCertManager.CustomCA.CACertificateRef.Name); err != nil {
				return err
			}
		default:
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.issuerType",
				Reason: fmt.Sprintf("issuerType %q is not yet supported in v1alpha1 (only CustomCA)", certs.BundledCertManager.IssuerType),
			}
		}
	default:
		return &validationError{
			Field:  "spec.ingress.certificates.provider",
			Reason: fmt.Sprintf("provider %q is not yet supported in v1alpha1 (only BundledCertManager)", certs.Provider),
		}
	}

	return nil
}

// checkCustomCASecret validates the CustomCA Secret reference in
// the operator namespace. Mirrors checkCASecret for Inline mode but
// expects tls.crt + tls.key (cert-manager's CA-issuer needs the
// private key), not ca.crt.
func (r *EducatesClusterConfigReconciler) checkCustomCASecret(ctx context.Context, name string) error {
	s := &corev1.Secret{}
	key := types.NamespacedName{Namespace: r.OperatorNamespace, Name: name}
	if err := r.Get(ctx, key, s); err != nil {
		if apierrors.IsNotFound(err) {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.customCA.caCertificateRef",
				Reason: fmt.Sprintf("Secret %s/%s not found", r.OperatorNamespace, name),
			}
		}
		return fmt.Errorf("get CustomCA Secret %s: %w", key, err)
	}
	for _, k := range []string{"tls.crt", "tls.key"} {
		if _, ok := s.Data[k]; !ok {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.customCA.caCertificateRef",
				Reason: fmt.Sprintf("Secret %s/%s is missing required key %q", r.OperatorNamespace, name, k),
			}
		}
	}
	return nil
}

// markCertificatesProgressing publishes a CertificatesReady=False
// condition while the cert-manager install pipeline is still
// converging. Reason is the kebab-case-ish PascalCase the rest of the
// reconciler uses; message is free-form.
func (r *EducatesClusterConfigReconciler) markCertificatesProgressing(obj *configv1alpha1.EducatesClusterConfig, reason, message string) {
	obj.Status.ObservedGeneration = obj.Generation
	obj.Status.Mode = obj.Spec.Mode
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionCertificatesReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
	// Aggregate Ready also stays False while any sub-condition is False.
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "Progressing",
		Message:            "Managed-mode reconciliation in progress",
		ObservedGeneration: obj.Generation,
	})
}

// markManagedPhase sets status.phase without touching conditions. The
// helper exists so reconcileManaged can advance the phase without
// duplicating the boilerplate from markReady/markDegraded — those are
// terminal-state writers.
func (r *EducatesClusterConfigReconciler) markManagedPhase(obj *configv1alpha1.EducatesClusterConfig, phase configv1alpha1.ClusterConfigPhase) {
	obj.Status.Phase = phase
}
