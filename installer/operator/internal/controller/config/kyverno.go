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

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// Kyverno install constants. Same shape as the other cluster
// services: dedicated namespace, helm release named after the
// chart, four Deployments to gate readiness on. Workload names
// derive from the chart's `kyverno.name` template (defaults to
// chart name "kyverno") plus the per-component suffix.
const (
	kyvernoNamespace   = "kyverno"
	kyvernoReleaseName = "kyverno"
)

// kyvernoDeployments are the four Deployments the chart installs at
// default values (admission, background, cleanup, reports
// controllers — reports-server is opt-in and not enabled). Readiness
// is gated on all four reporting Available=True.
var kyvernoDeployments = []string{
	"kyverno-admission-controller",
	"kyverno-background-controller",
	"kyverno-cleanup-controller",
	"kyverno-reports-controller",
}

// errKyvernoNotReady is the in-flight sentinel ensureKyvernoReady
// returns. Same shape as the other service-readiness sentinels.
var errKyvernoNotReady = errors.New("kyverno Deployments not yet Available")

// reconcileKyvernoPhase runs the Kyverno install pipeline:
//
//  1. helm install/upgrade from the vendored chart.
//  2. Wait for the four kyverno Deployments to report Available.
//
// Kyverno installs validating + mutating admission webhooks via the
// chart, with the cainjector-equivalent built into the
// admission-controller itself (it self-signs and injects its own
// caBundle on startup). Unlike cert-manager, the operator does NOT
// create any kyverno.io custom resources during install (policies
// are created elsewhere, by the session-manager when workshops
// deploy), so there is no admission-webhook race to classify on
// our SSA path — Kyverno's webhooks only act on resources that
// match installed policies, of which there are none at this stage.
//
// CRDs land in templates/ inside subcharts (charts/kyverno-api),
// not the special crds/ directory, so the in-memory helm test
// fake handles them without the SkipCRDs work-around.
//
// When provider != BundledKyverno (or neither policy engine ==
// Kyverno), the phase early-returns done=true.
func (r *EducatesClusterConfigReconciler) reconcileKyvernoPhase(ctx context.Context, log logr.Logger, obj *configv1alpha1.EducatesClusterConfig) (bool, ctrl.Result, error) {
	phaseStop := func(res ctrl.Result, err error) (bool, ctrl.Result, error) {
		return false, res, err
	}

	if !shouldInstallKyverno(obj) {
		return true, ctrl.Result{}, nil
	}

	if err := r.validateBundledKyverno(ctx, obj); err != nil {
		var verr *validationError
		if errors.As(err, &verr) {
			r.markDegraded(obj, verr.Field, verr.Reason)
			return phaseStop(ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, log, obj))
		}
		return phaseStop(ctrl.Result{}, err)
	}

	if err := r.reconcileKyverno(ctx, obj); err != nil {
		log.Error(err, "kyverno reconcile failed")
		r.markPolicyEnforcementProgressing(obj, "InstallFailed", err.Error())
		_ = r.updateStatusWithTransitionLog(ctx, log, obj)
		return phaseStop(ctrl.Result{}, err)
	}

	if err := r.ensureKyvernoReady(ctx); err != nil {
		if errors.Is(err, errKyvernoNotReady) {
			r.markPolicyEnforcementProgressing(obj, "WaitingForKyverno", "kyverno Deployments not yet Available")
			r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseInstalling)
			// Same cache-vs-watch race mitigation as the other
			// service-readiness gates.
			return false, ctrl.Result{RequeueAfter: 15 * time.Second}, r.updateStatusWithTransitionLog(ctx, log, obj)
		}
		return phaseStop(ctrl.Result{}, err)
	}

	r.markPolicyEnforcementReadyTrue(obj)
	return true, ctrl.Result{}, nil
}

// shouldInstallKyverno reports whether the operator is responsible
// for installing Kyverno. Two conditions:
//
//   - At least one of the cluster/workshop policy engines resolves
//     to Kyverno. If both engines are some other value (None,
//     PodSecurityStandards, OpenShiftSCC), there's no need for
//     Kyverno at all.
//   - The Kyverno sourcing block specifies Bundled provider.
//     External sourcing means a user-supplied Kyverno install is
//     expected; we just consume it.
func shouldInstallKyverno(obj *configv1alpha1.EducatesClusterConfig) bool {
	pe := obj.Spec.PolicyEnforcement
	if pe == nil {
		return false
	}
	usesKyverno := pe.ClusterPolicy.Engine == configv1alpha1.ClusterPolicyEngineKyverno ||
		pe.WorkshopPolicy.Engine == configv1alpha1.WorkshopPolicyEngineKyverno
	if !usesKyverno {
		return false
	}
	if pe.Kyverno == nil {
		return false
	}
	return pe.Kyverno.Provider == configv1alpha1.KyvernoProviderBundled
}

// reconcileKyverno performs the helm install/upgrade. Mirrors the
// cert-manager / Contour / external-dns shape.
func (r *EducatesClusterConfigReconciler) reconcileKyverno(ctx context.Context, owner *configv1alpha1.EducatesClusterConfig) error {
	chrt, err := vendoredcharts.Kyverno()
	if err != nil {
		return fmt.Errorf("load embedded kyverno chart: %w", err)
	}

	if err := r.ensureNamespace(ctx, kyvernoNamespace, nil, owner); err != nil {
		return err
	}

	hc, err := r.HelmClientFor(kyvernoNamespace)
	if err != nil {
		return fmt.Errorf("build helm client for %q: %w", kyvernoNamespace, err)
	}

	vals := renderKyvernoValues(owner)

	rel, err := hc.Status(kyvernoReleaseName)
	switch {
	case errors.Is(err, helm.ErrReleaseNotFound):
		if _, err := hc.Install(ctx, kyvernoReleaseName, chrt, vals); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if rel.Chart != nil && rel.Chart.Metadata != nil && rel.Chart.Metadata.Version != chrt.Metadata.Version {
			if _, err := hc.Upgrade(ctx, kyvernoReleaseName, chrt, vals); err != nil {
				return err
			}
		}
	}

	if owner.Status.BundledChartVersions == nil {
		owner.Status.BundledChartVersions = map[string]string{}
	}
	owner.Status.BundledChartVersions["kyverno"] = vendoredcharts.KyvernoChartVersion
	return nil
}

// renderKyvernoValues builds the values map. v1alpha1 is minimal —
// just plumbing the operational replica count and image-registry
// prefix; everything else uses chart defaults (4 controllers
// enabled, reports-server disabled, default resource limits). The
// chart surface is large and we deliberately don't expose more
// until concrete needs emerge.
func renderKyvernoValues(obj *configv1alpha1.EducatesClusterConfig) map[string]any {
	values := map[string]any{}

	if op := operationalForKyverno(obj); op != nil && op.Replicas != nil {
		// Kyverno's chart applies replicaCount per-controller. We
		// apply the same operational replica count to all four for
		// simplicity; users wanting per-component tuning can wait
		// for the freeform values pass-through follow-up.
		replicas := *op.Replicas
		values["admissionController"] = map[string]any{"replicas": replicas}
		values["backgroundController"] = map[string]any{"replicas": replicas}
		values["cleanupController"] = map[string]any{"replicas": replicas}
		values["reportsController"] = map[string]any{"replicas": replicas}
	}

	if obj.Spec.ImageRegistry != nil && obj.Spec.ImageRegistry.Prefix != "" {
		values["global"] = map[string]any{
			"image": map[string]any{
				"registry": obj.Spec.ImageRegistry.Prefix,
			},
		}
	}

	return values
}

// operationalForKyverno extracts the OperationalBlock without
// panicking if any of the parent fields is nil. Same shape as the
// guards we use in the Contour/external-dns paths.
func operationalForKyverno(obj *configv1alpha1.EducatesClusterConfig) *configv1alpha1.OperationalBlock {
	pe := obj.Spec.PolicyEnforcement
	if pe == nil || pe.Kyverno == nil || pe.Kyverno.Bundled == nil {
		return nil
	}
	return pe.Kyverno.Bundled.Operational
}

// validateBundledKyverno surfaces friendlier "not yet supported"
// errors for non-Kyverno policy engines that the operator can't
// install today. v1alpha1 supports only Kyverno; the
// PodSecurityStandards and OpenShiftSCC engines would require
// completely different reconcile logic and aren't in scope.
func (r *EducatesClusterConfigReconciler) validateBundledKyverno(_ context.Context, obj *configv1alpha1.EducatesClusterConfig) error {
	pe := obj.Spec.PolicyEnforcement
	if pe == nil {
		return nil
	}

	switch pe.ClusterPolicy.Engine {
	case configv1alpha1.ClusterPolicyEngineKyverno,
		configv1alpha1.ClusterPolicyEngineNone:
		// supported in v1alpha1.
	default:
		return &validationError{
			Field:  "spec.policyEnforcement.clusterPolicy.engine",
			Reason: fmt.Sprintf("engine %q is not yet supported in v1alpha1 (only Kyverno or None)", pe.ClusterPolicy.Engine),
		}
	}

	switch pe.WorkshopPolicy.Engine {
	case configv1alpha1.WorkshopPolicyEngineKyverno,
		configv1alpha1.WorkshopPolicyEngineNone:
		// supported.
	default:
		return &validationError{
			Field:  "spec.policyEnforcement.workshopPolicy.engine",
			Reason: fmt.Sprintf("engine %q is not yet supported in v1alpha1 (only Kyverno or None)", pe.WorkshopPolicy.Engine),
		}
	}

	if pe.Kyverno != nil && pe.Kyverno.Provider == configv1alpha1.KyvernoProviderExternal {
		return &validationError{
			Field:  "spec.policyEnforcement.kyverno.provider",
			Reason: "External Kyverno provider is not yet supported in v1alpha1",
		}
	}

	return nil
}

// ensureKyvernoReady gates the rest of the pipeline on the four
// kyverno Deployments reporting Available=True. Same shape as
// ensureCertManagerReady: a missing Deployment (404) maps to
// "not ready" rather than a hard error.
func (r *EducatesClusterConfigReconciler) ensureKyvernoReady(ctx context.Context) error {
	for _, name := range kyvernoDeployments {
		dep := &appsv1.Deployment{}
		key := types.NamespacedName{Namespace: kyvernoNamespace, Name: name}
		if err := r.Get(ctx, key, dep); err != nil {
			if apierrors.IsNotFound(err) {
				return errKyvernoNotReady
			}
			return fmt.Errorf("get Deployment %s: %w", key, err)
		}
		if !deploymentAvailable(dep) {
			return errKyvernoNotReady
		}
	}
	return nil
}

// cleanupKyverno unwinds the install: helm uninstall → kyverno
// namespace delete. helm-managed webhook configurations and CRDs
// cascade with the release uninstall. Idempotent.
func (r *EducatesClusterConfigReconciler) cleanupKyverno(ctx context.Context, _ *configv1alpha1.EducatesClusterConfig) error {
	hc, err := r.HelmClientFor(kyvernoNamespace)
	if err != nil {
		return fmt.Errorf("build helm client for cleanup: %w", err)
	}
	if err := hc.Uninstall(kyvernoReleaseName); err != nil {
		return fmt.Errorf("uninstall kyverno release: %w", err)
	}
	if err := r.deleteIfPresent(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: kyvernoNamespace},
	}); err != nil {
		return fmt.Errorf("delete kyverno namespace: %w", err)
	}
	return nil
}
