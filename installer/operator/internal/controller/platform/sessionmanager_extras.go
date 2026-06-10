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

package platform

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
)

// extrasIntent is the resolved install intent for an optional extra.
// The string values intentionally match the condition Reason field
// the reconciler publishes, so consumers reading the condition see
// the same vocabulary as the spec/mode the user wrote.
type extrasIntent int

const (
	// intentInstall: install or upgrade the release.
	intentInstall extrasIntent = iota
	// intentSkip: keep the release uninstalled (Mode=Disabled, or
	// Mode=Auto and the auto signal isn't satisfied). If a release
	// exists from a prior reconcile, drain it.
	intentSkip
	// intentRefuse: the user set Mode=Enabled but a prerequisite is
	// missing. Publish a clear NotReady condition and stop reconciling
	// the extra (the main session-manager install path continues).
	intentRefuse
)

// resolveNodeCATrust evaluates the tri-state mode against cluster
// state. Returns the intent plus a reason+message used for the
// status condition the reconciler publishes.
// reasonExtraInstalled is the shared condition reason for every
// install-outcome branch of the extras resolvers below.
const reasonExtraInstalled = "Installed"

func resolveNodeCATrust(obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) (extrasIntent, string, string) {
	mode := platformv1alpha1.ComponentModeAuto
	if obj.Spec.NodeCATrust != nil && obj.Spec.NodeCATrust.Mode != "" {
		mode = obj.Spec.NodeCATrust.Mode
	}
	hasCA := cfg.Status.Ingress != nil && cfg.Status.Ingress.CACertificateSecretRef != nil
	switch mode {
	case platformv1alpha1.ComponentModeDisabled:
		return intentSkip, "ModeDisabled",
			"nodeCATrust disabled by spec"
	case platformv1alpha1.ComponentModeEnabled:
		if !hasCA {
			return intentRefuse, "NodeCATrustMissingCA",
				"nodeCATrust mode=Enabled but EducatesClusterConfig.status.ingress.caCertificateSecretRef is not set"
		}
		return intentInstall, reasonExtraInstalled,
			"node-ca-injector installed (mode=Enabled)"
	case platformv1alpha1.ComponentModeAuto:
		fallthrough
	default:
		if hasCA {
			return intentInstall, reasonExtraInstalled,
				"node-ca-injector installed (mode=Auto: CA configured on the cluster)"
		}
		return intentSkip, "ModeAutoNoCA",
			"nodeCATrust skipped (mode=Auto: no CA configured on the cluster)"
	}
}

// resolveRemoteAccess evaluates remoteAccess mode against the
// presence of a LookupService singleton. NotFound on the lookup is
// folded into "Auto auto-skip" semantics rather than surfaced as an
// error.
func (r *SessionManagerReconciler) resolveRemoteAccess(ctx context.Context, obj *platformv1alpha1.SessionManager) (extrasIntent, string, string, error) {
	mode := platformv1alpha1.ComponentModeAuto
	if obj.Spec.RemoteAccess != nil && obj.Spec.RemoteAccess.Mode != "" {
		mode = obj.Spec.RemoteAccess.Mode
	}

	switch mode {
	case platformv1alpha1.ComponentModeDisabled:
		return intentSkip, "ModeDisabled",
			"remoteAccess disabled by spec", nil
	case platformv1alpha1.ComponentModeEnabled:
		return intentInstall, reasonExtraInstalled,
			"remote-access installed (mode=Enabled)", nil
	case platformv1alpha1.ComponentModeAuto:
		fallthrough
	default:
		hasLookup, err := r.lookupServiceExists(ctx)
		if err != nil {
			return intentSkip, "", "", err
		}
		if hasLookup {
			return intentInstall, reasonExtraInstalled,
				"remote-access installed (mode=Auto: LookupService present)", nil
		}
		return intentSkip, "ModeAutoNoLookupService",
			"remoteAccess skipped (mode=Auto: no LookupService CR)", nil
	}
}

// lookupServiceExists reports whether the singleton LookupService
// CR is present in the cluster. NotFound is the steady state when
// the user hasn't opted into cross-cluster federation; that's the
// expected signal for Auto skipping remote-access.
func (r *SessionManagerReconciler) lookupServiceExists(ctx context.Context) (bool, error) {
	ls := &platformv1alpha1.LookupService{}
	if err := r.Get(ctx, types.NamespacedName{Name: singletonName}, ls); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// reconcileExtra drives one optional subchart through its resolved
// intent. Returns nil on success (any of Install/Skip/Refuse counts
// as successfully reconciled — Refuse is a published condition, not
// a reconciler error). The caller is expected to call this after
// the main session-manager install lands; extras live in the same
// namespace and helm releases are unique by release-name, not by
// namespace.
//
// The intent → action mapping:
//   - Install: helm install (on absence) or upgrade (on presence).
//     Publish condition: type, Status=True, reason=`Installed`.
//   - Skip:    if a release exists from a previous reconcile, drain
//     it. Publish condition: type, Status=False, reason from
//     resolver. False because the extra isn't running; users grep
//     for True to confirm it's up.
//   - Refuse:  publish condition: type, Status=False, reason from
//     resolver, with a message pointing at the missing prerequisite.
//     No release work — the release wasn't running before, won't be
//     now.
func (r *SessionManagerReconciler) reconcileExtra(
	ctx context.Context,
	obj *platformv1alpha1.SessionManager,
	conditionType string,
	releaseName string,
	loadChart func() (*chart.Chart, error),
	renderValues func(*platformv1alpha1.SessionManager, *configv1alpha1.EducatesClusterConfig) map[string]any,
	cfg *configv1alpha1.EducatesClusterConfig,
	intent extrasIntent, reason, message string,
) error {
	log := logf.FromContext(ctx)
	hc, err := r.HelmClientFor(platformNamespace)
	if err != nil {
		return fmt.Errorf("build helm client for %s: %w", releaseName, err)
	}

	switch intent {
	case intentInstall:
		chrt, err := loadChart()
		if err != nil {
			return fmt.Errorf("load chart %s: %w", releaseName, err)
		}
		vals := renderValues(obj, cfg)
		if _, statusErr := hc.Status(releaseName); statusErr != nil {
			if statusErr == helm.ErrReleaseNotFound {
				log.Info("installing extra", "release", releaseName)
				if _, err := hc.Install(ctx, releaseName, chrt, vals); err != nil {
					setExtraCondition(obj, conditionType, metav1.ConditionFalse, "InstallFailed", err.Error())
					return fmt.Errorf("helm install %s: %w", releaseName, err)
				}
				setExtraCondition(obj, conditionType, metav1.ConditionTrue, reason, message)
				return nil
			}
			setExtraCondition(obj, conditionType, metav1.ConditionFalse, "InstallFailed", statusErr.Error())
			return fmt.Errorf("helm status %s: %w", releaseName, statusErr)
		}
		log.V(1).Info("upgrading extra", "release", releaseName)
		if _, err := hc.Upgrade(ctx, releaseName, chrt, vals); err != nil {
			setExtraCondition(obj, conditionType, metav1.ConditionFalse, "UpgradeFailed", err.Error())
			return fmt.Errorf("helm upgrade %s: %w", releaseName, err)
		}
		setExtraCondition(obj, conditionType, metav1.ConditionTrue, reason, message)
		return nil

	case intentSkip:
		// Idempotent uninstall: drains any release from a prior
		// reconcile when the intent flipped from Install to Skip.
		// helm.Uninstall classifies NotFound internally, so this is
		// a no-op when nothing is there.
		if err := hc.Uninstall(releaseName); err != nil {
			setExtraCondition(obj, conditionType, metav1.ConditionFalse, "UninstallFailed", err.Error())
			return fmt.Errorf("helm uninstall %s: %w", releaseName, err)
		}
		setExtraCondition(obj, conditionType, metav1.ConditionFalse, reason, message)
		return nil

	case intentRefuse:
		setExtraCondition(obj, conditionType, metav1.ConditionFalse, reason, message)
		return nil
	}
	return nil
}

// setExtraCondition writes a condition on the obj's status without
// going through the Update path. The caller publishes status in one
// final write at the end of Reconcile.
func setExtraCondition(obj *platformv1alpha1.SessionManager, condType string, status metav1.ConditionStatus, reason, message string) {
	// Lazy import of meta would create a cycle; inline the same
	// SetStatusCondition behaviour via condition list mutation.
	for i := range obj.Status.Conditions {
		if obj.Status.Conditions[i].Type == condType {
			c := &obj.Status.Conditions[i]
			if c.Status != status {
				c.LastTransitionTime = metav1.Now()
			}
			c.Status = status
			c.Reason = reason
			c.Message = message
			c.ObservedGeneration = obj.Generation
			return
		}
	}
	obj.Status.Conditions = append(obj.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

// renderNodeCAInjectorValues maps cluster config status into the
// node-ca-injector subchart's values shape. The subchart requires
// `clusterIngress.caCertificateRef.{name,namespace}`; without those
// it template-errors. The resolver above guarantees we only call
// this when the CA Secret is present in cluster config.
func renderNodeCAInjectorValues(obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) map[string]any {
	values := map[string]any{}

	if cfg.Status.ImageRegistry != nil && cfg.Status.ImageRegistry.Prefix != "" {
		host, ns := splitImageRegistryPrefix(cfg.Status.ImageRegistry.Prefix)
		values["development"] = map[string]any{
			"imageRegistry": map[string]any{
				"host":      host,
				"namespace": ns,
			},
		}
	}
	if cfg.Status.ImageRegistry != nil && len(cfg.Status.ImageRegistry.PullSecrets) > 0 {
		refs := make([]any, 0, len(cfg.Status.ImageRegistry.PullSecrets))
		for _, ref := range cfg.Status.ImageRegistry.PullSecrets {
			refs = append(refs, map[string]any{"name": ref.Name})
		}
		values["imagePullSecrets"] = refs
	}

	caRef := cfg.Status.Ingress.CACertificateSecretRef
	values["clusterIngress"] = map[string]any{
		"caCertificateRef": map[string]any{
			"name":      caRef.Name,
			"namespace": caRef.Namespace,
		},
	}
	_ = obj // CR has no per-extra overrides today; reserved for follow-up
	return values
}

// renderRemoteAccessValues maps cluster config status into the
// remote-access subchart's values shape. The subchart has no
// configurable knobs in v0.1.0 — pull secrets aren't even needed
// because no image is deployed (it's just RBAC + a token Secret).
// Returning an empty map keeps the chart on its defaults.
func renderRemoteAccessValues(_ *platformv1alpha1.SessionManager, _ *configv1alpha1.EducatesClusterConfig) map[string]any {
	return map[string]any{}
}

// uninstallExtraQuietly drops a release if it exists; used by the
// SessionManager cleanup path to drain the two optional extras
// alongside the main release on finalizer drain. Logs but does not
// propagate uninstall errors — the main release uninstall is the
// only one that must succeed cleanly, and a stale extra release
// would only block future reconciles, not break the cluster.
func (r *SessionManagerReconciler) uninstallExtraQuietly(log logr.Logger, releaseName string) {
	hc, err := r.HelmClientFor(platformNamespace)
	if err != nil {
		log.Error(err, "build helm client for extras cleanup", "release", releaseName)
		return
	}
	if err := hc.Uninstall(releaseName); err != nil {
		log.Error(err, "uninstall extra release", "release", releaseName)
	}
}
