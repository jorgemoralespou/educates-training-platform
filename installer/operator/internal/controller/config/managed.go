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
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

// reconcileManaged drives Phase 2 Managed-mode reconciliation:
//
//  1. Validate spec fields (cross-resource checks the CRD's CEL rules
//     cannot express; not-yet-supported provider errors).
//  2. Install/upgrade the cert-manager chart from the vendored tarball
//     and record the chart version in status.
//  3. Gate on cert-manager Deployment availability (Phase 2 readiness
//     contract — see follow-up-issues.md for the synthetic-admission
//     hardening option).
//  4. Copy the CustomCA Secret into cert-manager's namespace, apply the
//     CA-typed ClusterIssuer, and apply the wildcard Certificate via
//     SSA.
//  5. Once the Certificate reports Ready=True, publish status.ingress
//     and flip CertificatesReady (and the aggregate Ready) to True.
//
// Other cluster services (Contour, external-dns, Kyverno) follow the
// same shape in Phase 3.
func (r *EducatesClusterConfigReconciler) reconcileManaged(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if err := r.validateManaged(ctx, obj); err != nil {
		var verr *validationError
		if errors.As(err, &verr) {
			r.markDegraded(obj, verr.Field, verr.Reason)
			return ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, log, obj)
		}
		return ctrl.Result{}, err
	}

	if err := r.reconcileCertManager(ctx, obj); err != nil {
		log.Error(err, "cert-manager reconcile failed")
		r.markCertificatesProgressing(obj, "InstallFailed", err.Error())
		_ = r.updateStatusWithTransitionLog(ctx, log, obj)
		return ctrl.Result{}, err
	}

	// Gate the rest of the pipeline on cert-manager being live. A
	// not-ready signal is published as a progressing condition; the
	// Deployment watch will re-trigger reconcile when Availability
	// flips, so no explicit requeue is needed.
	if err := r.ensureCertManagerReady(ctx); err != nil {
		if errors.Is(err, errCertManagerNotReady) {
			r.markCertificatesProgressing(obj, "WaitingForCertManager", "cert-manager Deployments not yet Available")
			r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseInstalling)
			return ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, log, obj)
		}
		return ctrl.Result{}, err
	}

	// CustomCA Secret → cert-manager namespace, then ClusterIssuer, then
	// wildcard Certificate. Each helper is idempotent (SSA) so re-running
	// after a partial failure converges.
	customCARef := obj.Spec.Ingress.Certificates.BundledCertManager.CustomCA.CACertificateRef.Name
	if err := r.ensureCustomCASecretCopy(ctx, obj, customCARef); err != nil {
		if isCertManagerCRDMissingErr(err) {
			return r.handleCertManagerCRDsMissing(ctx, obj, log, err)
		}
		r.markCertificatesProgressing(obj, "CustomCACopyFailed", err.Error())
		_ = r.updateStatusWithTransitionLog(ctx, log, obj)
		return ctrl.Result{}, err
	}
	if err := r.ensureClusterIssuer(ctx, obj); err != nil {
		if isCertManagerCRDMissingErr(err) {
			return r.handleCertManagerCRDsMissing(ctx, obj, log, err)
		}
		if isWebhookNotReadyErr(err) {
			return r.handleWebhookNotReady(ctx, obj, log, "ClusterIssuer", err)
		}
		r.markCertificatesProgressing(obj, "ClusterIssuerApplyFailed", err.Error())
		_ = r.updateStatusWithTransitionLog(ctx, log, obj)
		return ctrl.Result{}, err
	}
	if err := r.ensureWildcardCertificate(ctx, obj, obj.Spec.Ingress.Domain); err != nil {
		if isCertManagerCRDMissingErr(err) {
			return r.handleCertManagerCRDsMissing(ctx, obj, log, err)
		}
		if isWebhookNotReadyErr(err) {
			return r.handleWebhookNotReady(ctx, obj, log, "Certificate", err)
		}
		r.markCertificatesProgressing(obj, "CertificateApplyFailed", err.Error())
		_ = r.updateStatusWithTransitionLog(ctx, log, obj)
		return ctrl.Result{}, err
	}

	ready, err := r.certificateReady(ctx)
	if err != nil {
		if isCertManagerCRDMissingErr(err) {
			return r.handleCertManagerCRDsMissing(ctx, obj, log, err)
		}
		return ctrl.Result{}, err
	}
	if !ready {
		r.markCertificatesProgressing(obj, "WaitingForCertificate", "wildcard Certificate not yet Ready")
		r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseInstalling)
		return ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, log, obj)
	}

	r.markManagedReady(obj)
	return ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, log, obj)
}

// handleCertManagerCRDsMissing handles a NoMatchError (or 404
// "kind not found") on a cert-manager.io kind. The error has two
// possible root causes and we must distinguish them via a fresh
// discovery probe — the operator's local RESTMapper alone can't
// tell us which:
//
//  1. **CRDs really missing.** End-of-life teardown (helm uninstall
//     just removed them) or a user out-of-band `kubectl delete crd`.
//     Surface as a clean Degraded condition with a 60s requeue.
//
//  2. **CRDs present but the operator's RESTMapper is stale.** Most
//     common during Managed-mode install bootstrap: the operator
//     pod was started before cert-manager existed, so its mapper
//     cached "no cert-manager.io group". After helm install lands
//     the CRDs, the mapper doesn't auto-refresh — every typed-client
//     call to a cert-manager kind returns NoMatchError until
//     something invalidates the cache. CRDWatcher.registerWatch
//     does that whenever it activates a watch, but there's a
//     window of up to PollInterval where the SSA path can race
//     ahead. Recovery: Reset the mapper and retry shortly. The
//     condition stays at its prior state (typically Progressing
//     /WaitingForCertManager), so the user doesn't see a
//     spurious Degraded blip.
//
// Why not just always Reset+retry: in the genuinely-missing case,
// resetting the mapper does nothing useful (the next call still
// returns NoMatchError after re-discovery), and the user needs
// the Degraded signal to know to reinstall. Discovery is the
// authoritative test.
//
// Note: this only quiets the operator's *own* error paths. The
// underlying controller-runtime Kind source (registered by
// CRDWatcher when the CRDs were still present) keeps logging a
// retry-loop error at 10s intervals because controller-runtime has
// no public API to remove a registered Source. Captured as a
// follow-up — see docs/architecture/follow-up-issues.md.
func (r *EducatesClusterConfigReconciler) handleCertManagerCRDsMissing(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig, log logr.Logger, cause error) (ctrl.Result, error) {
	if r.certManagerCRDsActuallyPresent() {
		// Mapper-staleness path. Reset and retry shortly; the user
		// shouldn't see Degraded for a transient bootstrap race.
		log.Info("RESTMapper cache is stale; cert-manager CRDs are present in discovery — resetting mapper and retrying",
			"cause", cause.Error())
		if resetter, ok := r.RESTMapper().(interface{ Reset() }); ok {
			resetter.Reset()
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	log.Info("cert-manager.io CRDs are no longer present in the cluster; operator state is Degraded",
		"cause", cause.Error())
	r.markCertificatesProgressing(obj, "CertManagerCRDsMissing",
		"cert-manager.io CRDs are no longer present in the cluster; reinstall cert-manager or delete this EducatesClusterConfig")
	r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseDegraded)
	if err := r.updateStatusWithTransitionLog(ctx, log, obj); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

// certManagerCRDsActuallyPresent does a fresh discovery probe (not
// going through the operator's potentially-stale RESTMapper) to
// check whether cert-manager.io/v1 carries the ClusterIssuer and
// Certificate kinds we'd otherwise mark Degraded for. If the
// Discovery client isn't set (tests that don't wire it), defaults
// to "not present" — the test envtest can register CRDs through
// envtest infra and the local mapper sees them, so this path only
// fires when something actually went wrong.
func (r *EducatesClusterConfigReconciler) certManagerCRDsActuallyPresent() bool {
	if r.Discovery == nil {
		return false
	}
	rl, err := r.Discovery.ServerResourcesForGroupVersion("cert-manager.io/v1")
	if err != nil || rl == nil {
		return false
	}
	var sawClusterIssuer, sawCertificate bool
	for _, res := range rl.APIResources {
		switch res.Kind {
		case "ClusterIssuer":
			sawClusterIssuer = true
		case "Certificate":
			sawCertificate = true
		}
	}
	return sawClusterIssuer && sawCertificate
}

// handleWebhookNotReady surfaces the "cert-manager webhook isn't
// answering yet" failure mode as a clean progressing condition with a
// friendly INFO log line and a short RequeueAfter, suppressing the
// error-return path that would otherwise dump a stack trace at ERROR.
// kind is the resource the operator was trying to apply
// ("ClusterIssuer" or "Certificate") and shows up in the log line so
// the cause is obvious. See certmanager.go::isWebhookNotReadyErr for
// the substring rationale; the proper fix is the synthetic admission
// probe captured in follow-up-issues.md.
func (r *EducatesClusterConfigReconciler) handleWebhookNotReady(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig, log logr.Logger, kind string, cause error) (ctrl.Result, error) {
	log.Info("cert-manager webhook not yet routable; will retry shortly",
		"kind", kind,
		"cause", cause.Error())
	r.markCertificatesProgressing(obj, "WaitingForWebhook",
		fmt.Sprintf("apply of %s blocked: cert-manager admission webhook not yet serving (cainjector caBundle propagation in flight)", kind))
	r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseInstalling)
	if err := r.updateStatusWithTransitionLog(ctx, log, obj); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

// cleanupManaged tears down Phase 2's installed cluster services in
// reverse install order:
//
//  1. Wildcard Certificate (cert-manager is still running, so it
//     processes the deletion and revokes the issued Secret cleanly).
//  2. ClusterIssuer.
//  3. Copied CustomCA Secret in cert-manager namespace.
//  4. Helm release "cert-manager" (uninstalls Deployments, CRDs,
//     webhook configurations the chart owns).
//  5. cert-manager namespace.
//
// Each step ignores not-found so retried reconciles after partial
// failure re-attempt only what's still present.
func (r *EducatesClusterConfigReconciler) cleanupManaged(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) error {
	if err := r.deleteIfPresent(ctx, &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{Namespace: r.OperatorNamespace, Name: wildcardCertificate},
	}); err != nil {
		return fmt.Errorf("delete wildcard Certificate: %w", err)
	}
	if err := r.deleteIfPresent(ctx, &cmv1.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{Name: wildcardClusterIssuer},
	}); err != nil {
		return fmt.Errorf("delete ClusterIssuer: %w", err)
	}
	if err := r.deleteIfPresent(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: certManagerNamespace, Name: customCASecretName},
	}); err != nil {
		return fmt.Errorf("delete copied CustomCA Secret: %w", err)
	}

	// Helm uninstall is also idempotent in the wrapper (IgnoreNotFound
	// on the action). Skip when the release was never created — e.g.,
	// validation failed before reconcileCertManager ran.
	hc, err := r.HelmClientFor(certManagerNamespace)
	if err != nil {
		return fmt.Errorf("build helm client for cleanup: %w", err)
	}
	if err := hc.Uninstall(certManagerReleaseName); err != nil {
		return fmt.Errorf("uninstall cert-manager release: %w", err)
	}

	if err := r.deleteIfPresent(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: certManagerNamespace},
	}); err != nil {
		return fmt.Errorf("delete cert-manager namespace: %w", err)
	}
	return nil
}

// deleteIfPresent issues a Delete and swallows the two error
// classes that mean "already gone from the operator's perspective":
//   - IsNotFound: the named object no longer exists.
//   - IsNoMatchError: the kind itself no longer exists (CRD removed,
//     e.g., after helm uninstall earlier in this same drain pass).
//
// Both states are functionally terminal for finalizer drain — the
// resource we wanted to delete is gone. Returning an error from
// either would block the rest of the drain on something that's
// already in the desired state.
func (r *EducatesClusterConfigReconciler) deleteIfPresent(ctx context.Context, obj client.Object) error {
	if err := r.Delete(ctx, obj); err != nil {
		if apierrors.IsNotFound(err) || isCertManagerCRDMissingErr(err) {
			return nil
		}
		return err
	}
	return nil
}

// markManagedReady publishes the inter-CR ingress contract and flips
// CertificatesReady + Ready to True. Mirrors markReady (Inline) but
// sources the contract from cert-manager-issued resources rather than
// user-declared references.
func (r *EducatesClusterConfigReconciler) markManagedReady(obj *configv1alpha1.EducatesClusterConfig) {
	obj.Status.ObservedGeneration = obj.Generation
	obj.Status.Phase = configv1alpha1.ClusterConfigPhaseReady
	obj.Status.Mode = obj.Spec.Mode
	obj.Status.Ingress = &configv1alpha1.StatusIngress{
		Domain:           obj.Spec.Ingress.Domain,
		IngressClassName: obj.Spec.Ingress.IngressClassName,
		WildcardCertificateSecretRef: configv1alpha1.NamespacedSecretRef{
			Namespace: r.OperatorNamespace,
			Name:      wildcardTLSSecretName,
		},
		ClusterIssuerRef: &configv1alpha1.LocalObjectReference{
			Name: wildcardClusterIssuer,
		},
	}

	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionCertificatesReady,
		Status:             metav1.ConditionTrue,
		Reason:             "CertificateIssued",
		Message:            "wildcard Certificate is Ready",
		ObservedGeneration: obj.Generation,
	})
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "ManagedServicesReady",
		Message:            "Managed-mode cluster services are ready",
		ObservedGeneration: obj.Generation,
	})
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
// cert-manager chart. Image-registry-prefix rewriting and operational
// overrides land alongside the rest of the Managed-mode CR fields in
// later commits; today the function only sets values that are needed
// to make the Helm install behave well under operator-driven
// reconciliation.
//
// crds.enabled=true: cert-manager v1.18+ defaults its CRDs to OFF
// (`crds.enabled: false` in chart values.yaml — verified against
// the vendored v1.20.2 tarball). Without this override, helm-install
// succeeds, cert-manager pods come up, the operator's deployment
// readiness gate passes — and then every typed SSA call against
// ClusterIssuer/Certificate returns NoMatchError forever because
// the CRDs were never applied. Opt-in.
//
// crds.keep=false: by default the chart annotates CRDs with
// "helm.sh/resource-policy: keep" so `helm uninstall` leaves them
// in the cluster. For the operator's Managed-mode lifecycle that
// inverts what we want — the EducatesClusterConfig owns the
// cert-manager install end-to-end, and on teardown the user
// expects everything to go away. Setting keep=false makes
// `helm uninstall` cascade-delete the CRDs (which in turn
// cascades to any remaining cert-manager.io resources).
//
// startupapicheck is a post-install Helm hook that pings the
// cert-manager webhook to confirm the API is serving. We disable it
// for two reasons:
//
//  1. The hook's "is the webhook routable?" check duplicates the
//     readiness gate we already run after install
//     (ensureCertManagerReady + the WaitingForWebhook classification
//     on SSA failures). Having both means we pay for the same probe
//     twice on every fresh install.
//
//  2. The hook is wrapped in Helm's WaitStrategy timeout (default 5
//     minutes). If cainjector hasn't injected the caBundle by then,
//     the install returns a hard error and the release is left
//     "failed" — turning a transient bootstrap race into a deadlock
//     that needs a manual `helm uninstall`. With the hook disabled,
//     the install completes fast and the operator's own retry loop
//     converges on its own cadence.
//
// Kept as a standalone function so values-shape changes don't ripple
// through reconcile control flow.
func renderCertManagerValues(_ *configv1alpha1.EducatesClusterConfig) map[string]any {
	return map[string]any{
		"crds": map[string]any{
			"enabled": true,
			"keep":    false,
		},
		"startupapicheck": map[string]any{
			"enabled": false,
		},
	}
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
