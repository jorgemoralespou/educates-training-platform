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

// Contour install constants. Like cert-manager, Contour gets its
// own namespace; the chart's resources land there, and the
// operator manages namespace lifecycle (idempotent create + label
// + owner-ref via ensureNamespace; cascade delete on cleanup).
const (
	contourNamespace   = "contour"
	contourReleaseName = "contour"
)

// Workload names installed by the chart. Verified against
// contour-0.5.0 templates: workload names are produced via
//
//	{{ printf "%s-contour" (include "common.names.fullname" .) }}
//	{{ printf "%s-envoy"   (include "common.names.fullname" .) }}
//
// where common.names.fullname resolves to the release name when
// the release name already contains the chart name (Bitnami's
// standard fullname helper). With release name "contour" and
// chart name "contour", fullname == "contour", so the final
// resource names are `contour-contour` (Deployment) and
// `contour-envoy` (DaemonSet). Readiness is gated on the
// Deployment reporting Available and the DaemonSet reporting
// NumberReady >= DesiredNumberScheduled.
const (
	contourControllerDeployment = "contour-contour"
	envoyDaemonSet              = "contour-envoy"
)

// errContourNotReady is the sentinel ensureContourReady returns
// when the install is in flight but not yet fully serving. Same
// shape as errCertManagerNotReady so the phase function can
// classify cleanly.
var errContourNotReady = errors.New("contour install not yet Available")

// reconcileContourPhase runs the Contour install pipeline:
//
//  1. helm install/upgrade the vendored Contour chart.
//  2. Wait for the contour Deployment + envoy DaemonSet to be Ready.
//
// Contour does NOT install an admission webhook (verified against
// the 0.5.0 chart templates), so there's no cainjector-style
// bootstrap race to classify — the only "waiting" state is the
// workload rollout. CRDs (HTTPProxy, TLSCertificateDelegation,
// ExtensionService) are installed by the chart with
// manageCRDs:true; the operator does not Get/Create/Update them
// directly, so no CRDWatcher entries are needed.
//
// When provider != BundledContour the phase early-returns
// done=true: validation has already required the user-supplied
// IngressClass to exist, status.ingress.ingressClassName gets
// populated by markManagedReady downstream, and there's nothing
// to install or undo.
func (r *EducatesClusterConfigReconciler) reconcileContourPhase(ctx context.Context, log logr.Logger, obj *configv1alpha1.EducatesClusterConfig) (bool, ctrl.Result, error) {
	phaseStop := func(res ctrl.Result, err error) (bool, ctrl.Result, error) {
		return false, res, err
	}

	if !shouldInstallContour(obj) {
		return true, ctrl.Result{}, nil
	}

	if err := r.reconcileContour(ctx, obj); err != nil {
		log.Error(err, "contour reconcile failed")
		r.markIngressProgressing(obj, "InstallFailed", err.Error())
		_ = r.updateStatusWithTransitionLog(ctx, log, obj)
		return phaseStop(ctrl.Result{}, err)
	}

	if err := r.ensureContourReady(ctx); err != nil {
		if errors.Is(err, errContourNotReady) {
			r.markIngressProgressing(obj, "WaitingForContour", "contour Deployment + envoy DaemonSet not yet Ready")
			r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseInstalling)
			// RequeueAfter is required here: with only one Deployment
			// + one DaemonSet, Contour's final Available/Ready
			// transitions produce only ~2 watch events. If this
			// reconcile observed not-yet-Ready from a cache snapshot
			// taken a hair before the apiserver-side transition,
			// the workload watch may not fire again (the workload's
			// status is now stable) and we'd be stuck. cert-manager
			// avoids this naturally by staggering 3 Deployments;
			// Contour can't. 15s of self-poll matches the
			// WaitingForWebhook pattern.
			return false, ctrl.Result{RequeueAfter: 15 * time.Second}, r.updateStatusWithTransitionLog(ctx, log, obj)
		}
		return phaseStop(ctrl.Result{}, err)
	}

	r.markIngressReadyTrue(obj)
	return true, ctrl.Result{}, nil
}

// shouldInstallContour reports whether the operator is responsible
// for installing Contour. False when the user picked
// ExternalIngressController — validation has already ensured the
// user's IngressClass exists, and the operator only consumes the
// name via status.ingress.ingressClassName.
func shouldInstallContour(obj *configv1alpha1.EducatesClusterConfig) bool {
	if obj.Spec.Ingress == nil {
		return false
	}
	return obj.Spec.Ingress.Controller.Provider == configv1alpha1.IngressControllerProviderBundledContour
}

// reconcileContour ensures the Contour Helm release exists,
// installing from the vendored tarball on first sight. Mirrors
// reconcileCertManager's Status → Install/Upgrade routing.
func (r *EducatesClusterConfigReconciler) reconcileContour(ctx context.Context, owner *configv1alpha1.EducatesClusterConfig) error {
	chrt, err := vendoredcharts.Contour()
	if err != nil {
		return fmt.Errorf("load embedded contour chart: %w", err)
	}

	if err := r.ensureNamespace(ctx, contourNamespace, nil, owner); err != nil {
		return err
	}

	hc, err := r.HelmClientFor(contourNamespace)
	if err != nil {
		return fmt.Errorf("build helm client for %q: %w", contourNamespace, err)
	}

	vals := renderContourValues(owner)

	rel, err := hc.Status(contourReleaseName)
	switch {
	case errors.Is(err, helm.ErrReleaseNotFound):
		if _, err := hc.Install(ctx, contourReleaseName, chrt, vals); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		// Release exists. Upgrade only if the embedded chart version
		// has drifted from what was last installed.
		if rel.Chart != nil && rel.Chart.Metadata != nil && rel.Chart.Metadata.Version != chrt.Metadata.Version {
			if _, err := hc.Upgrade(ctx, contourReleaseName, chrt, vals); err != nil {
				return err
			}
		}
	}

	if owner.Status.BundledChartVersions == nil {
		owner.Status.BundledChartVersions = map[string]string{}
	}
	owner.Status.BundledChartVersions["contour"] = vendoredcharts.ContourChartVersion
	return nil
}

// renderContourValues builds the values map passed to the Contour
// chart. Driven by:
//
//   - spec.ingress.ingressClassName → contour.ingressClass.name
//     (the chart creates the IngressClass with this name and
//     marks it as default).
//   - spec.ingress.controller.bundledContour.envoyServiceType →
//     envoy.service.type. Defaults to LoadBalancer at the CRD
//     level (kubebuilder default); we re-default defensively here
//     for the case where the field somehow arrives empty.
//   - spec.ingress.controller.bundledContour.operational.replicas →
//     contour.replicaCount.
//   - spec.imageRegistry.prefix → global.imageRegistry (chart
//     supports a global registry override out of the box).
//
// Defaults are conservative: 1 replica unless the user asks for
// more; ingressClass.create=true + ingressClass.default=true so
// fresh installs work without users needing to mark the class
// default elsewhere.
//
// Note: the operator is intentionally **infra-agnostic** — it does
// not branch on spec.infrastructure.provider here. Cluster
// topology that affects the chart values (service type, etc.) is
// the user's explicit declaration via the bundledContour block.
func renderContourValues(obj *configv1alpha1.EducatesClusterConfig) map[string]any {
	ingressClassName := obj.Spec.Ingress.IngressClassName

	var replicas int32 = 1
	envoyServiceType := configv1alpha1.EnvoyServiceTypeLoadBalancer
	if bc := obj.Spec.Ingress.Controller.BundledContour; bc != nil {
		if bc.Operational != nil && bc.Operational.Replicas != nil {
			replicas = *bc.Operational.Replicas
		}
		if bc.EnvoyServiceType != "" {
			envoyServiceType = bc.EnvoyServiceType
		}
	}

	values := map[string]any{
		"contour": map[string]any{
			"replicaCount": replicas,
			"ingressClass": map[string]any{
				"name":    ingressClassName,
				"create":  true,
				"default": true,
			},
			"manageCRDs": true,
		},
		"envoy": map[string]any{
			"service": map[string]any{
				"type": string(envoyServiceType),
			},
		},
	}

	if obj.Spec.ImageRegistry != nil && obj.Spec.ImageRegistry.Prefix != "" {
		values["global"] = map[string]any{
			"imageRegistry": obj.Spec.ImageRegistry.Prefix,
		}
	}

	return values
}

// ensureContourReady gates the rest of the pipeline on Contour's
// data plane being live. Two checks:
//
//  1. The contour Deployment has Available=True. A missing
//     Deployment (404) maps to "not ready" rather than a hard
//     error — Helm may not have finished applying manifests yet.
//  2. The envoy DaemonSet has DesiredNumberScheduled > 0 and
//     NumberReady >= DesiredNumberScheduled. DaemonSets don't have
//     an Available condition the way Deployments do; the canonical
//     "ready" signal is "all desired pods ready". A 0/0 state is
//     treated as not-ready — a fresh cluster with no node-readiness
//     hasn't rolled out the DaemonSet yet.
func (r *EducatesClusterConfigReconciler) ensureContourReady(ctx context.Context) error {
	dep := &appsv1.Deployment{}
	depKey := types.NamespacedName{Namespace: contourNamespace, Name: contourControllerDeployment}
	if err := r.Get(ctx, depKey, dep); err != nil {
		if apierrors.IsNotFound(err) {
			return errContourNotReady
		}
		return fmt.Errorf("get Deployment %s: %w", depKey, err)
	}
	if !deploymentAvailable(dep) {
		return errContourNotReady
	}

	ds := &appsv1.DaemonSet{}
	dsKey := types.NamespacedName{Namespace: contourNamespace, Name: envoyDaemonSet}
	if err := r.Get(ctx, dsKey, ds); err != nil {
		if apierrors.IsNotFound(err) {
			return errContourNotReady
		}
		return fmt.Errorf("get DaemonSet %s: %w", dsKey, err)
	}
	if ds.Status.DesiredNumberScheduled == 0 || ds.Status.NumberReady < ds.Status.DesiredNumberScheduled {
		return errContourNotReady
	}

	return nil
}

// cleanupContour unwinds the Contour install in reverse order:
// helm uninstall (which removes the Deployment, DaemonSet,
// Service, IngressClass, and the chart-managed CRDs) → contour
// namespace delete. The chart's IngressClass uses Helm's default
// resource policy (managed), so helm uninstall cascades it.
//
// Idempotent: re-running after partial drain re-attempts only
// what's still present. helm.Uninstall has IgnoreNotFound on the
// underlying action.
func (r *EducatesClusterConfigReconciler) cleanupContour(ctx context.Context, _ *configv1alpha1.EducatesClusterConfig) error {
	hc, err := r.HelmClientFor(contourNamespace)
	if err != nil {
		return fmt.Errorf("build helm client for cleanup: %w", err)
	}
	if err := hc.Uninstall(contourReleaseName); err != nil {
		return fmt.Errorf("uninstall contour release: %w", err)
	}

	if err := r.deleteIfPresent(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: contourNamespace},
	}); err != nil {
		return fmt.Errorf("delete contour namespace: %w", err)
	}
	return nil
}
