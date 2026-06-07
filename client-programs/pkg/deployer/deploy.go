// Package deployer is the v4 install path: load a CLI config, translate
// to operator chart values + four platform CRs, install the operator
// chart via Helm SDK, server-side-apply the CRs in dependency order, and
// wait for each to be Ready=True.
//
// Walking skeleton scope: the happy path works end-to-end on a kind
// cluster. Polish (richer progress reporting, dry-run, rollback) is for
// follow-up commits in step 5.
package deployer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/apply"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/chart"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/crds"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/helm"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/prereq"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/wait"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

const (
	// OperatorNamespace is where the educates-installer helm release
	// lives. Matches the chart's recommended deploy namespace and the
	// samples in installer/samples/.
	OperatorNamespace = "educates-installer"

	// OperatorReleaseName is the helm release name.
	OperatorReleaseName = "educates-installer"

	// DefaultTimeout caps the wait on each CR's Ready=True condition.
	DefaultTimeout = 5 * time.Minute
)

// Options configures a deploy run.
type Options struct {
	// Getter is the kubectl-style RESTClientGetter — typically a
	// configured genericclioptions.ConfigFlags from cobra.
	Getter genericclioptions.RESTClientGetter

	// Out is where progress lines are written. Pass cmd.OutOrStdout()
	// from cobra; pass io.Discard from tests.
	Out io.Writer

	// HelmLog receives helm SDK debug output. io.Discard by default.
	HelmLog io.Writer

	// Timeout overrides DefaultTimeout per CR wait.
	Timeout time.Duration

	// SkipPrereqCheck bypasses the educates-custom-ca Secret existence
	// check. Set when the caller has already verified, or for advanced
	// users who manage the Secret asynchronously.
	SkipPrereqCheck bool

	// SkipCRDApply bypasses the operator CRD apply step. The default
	// is to push CRDs from the embedded chart before helm-install so a
	// CRD shape change reaches the cluster on re-deploy (helm itself
	// only installs CRDs on first install, never on upgrade). Set true
	// when the user manages CRDs out of band (GitOps, separate
	// kubectl apply, etc.).
	SkipCRDApply bool

	// SyncLocalSecrets, when true, copies <data-home>/secrets/*.yaml into
	// the cluster's 'educates-secrets' namespace before applying the
	// platform CRs. Matches the v3 laptop flow: cached CA/TLS material
	// is pushed at deploy time, ECC's caCertificateRef points there.
	// Only meaningful for EducatesLocalConfig deploys.
	SyncLocalSecrets bool

	// Progress is the structured progress reporter. When nil, plain
	// io.Discard-backed reporter is used (no progress output, but the
	// helpers still call into it so the call-site code is uniform).
	// Cmd code typically passes progress.NewForStdout(...) to render
	// to the user's terminal with TTY-aware overwriting.
	Progress progress.Reporter
}

// Deploy executes the install pipeline against the cluster reachable
// via opts.Getter:
//
//  1. Helm install/upgrade the operator chart in the operator namespace.
//  2. Apply EducatesClusterConfig → wait Ready.
//  3. Verify the educates-custom-ca prerequisite Secret (unless skipped).
//  4. Apply SecretsManager → wait Ready.
//  5. Apply LookupService (if present) → wait Ready.
//  6. Apply SessionManager → wait Ready.
//
// Returns the last LookupService/SessionManager objects observed so the
// caller can print URLs from .status.
func Deploy(ctx context.Context, out *translator.Output, opts Options) error {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.HelmLog == nil {
		opts.HelmLog = io.Discard
	}
	if opts.Timeout == 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.Progress == nil {
		opts.Progress = progress.New(io.Discard, 0, false)
	}

	// Note-class lines (no step counter) for the setup operations
	// that aren't part of the install sequence proper.
	if opts.SyncLocalSecrets {
		opts.Progress.Note("syncing cached local secrets to cluster")
		if err := syncLocalSecrets(opts.Getter); err != nil {
			return err
		}
	}

	chrt, err := chart.Load()
	if err != nil {
		return fmt.Errorf("load embedded chart: %w", err)
	}
	// Applier needs to exist before the CRD step; hoist before helm.
	applier, err := apply.New(opts.Getter)
	if err != nil {
		return err
	}
	waiter, err := wait.New(opts.Getter)
	if err != nil {
		return err
	}

	// 1. CRDs.
	if !opts.SkipCRDApply {
		step := opts.Progress.Start("apply CRDs")
		applied, err := crds.Apply(ctx, applier, chrt)
		if err != nil {
			step.Fail(err)
			return err
		}
		step.Done(fmt.Sprintf("%d applied", len(applied)))
	}

	// 2. Helm install/upgrade the operator chart.
	step := opts.Progress.Start(fmt.Sprintf("helm upgrade --install %s", OperatorReleaseName))
	helmClient, err := helm.New(opts.Getter, OperatorNamespace, opts.HelmLog)
	if err != nil {
		step.Fail(err)
		return err
	}
	if _, err := helmClient.UpgradeOrInstall(ctx, OperatorReleaseName, chrt, out.OperatorChartValues); err != nil {
		step.Fail(err)
		return err
	}
	step.Done("released")

	// 3. Apply EducatesClusterConfig + wait.
	if err := applyAndWaitStep(ctx, opts, applier, waiter, out.EducatesClusterConfig, "EducatesClusterConfig"); err != nil {
		return err
	}

	// 4. Prereq check — only meaningful when the caller didn't sync
	//    local secrets. With SyncLocalSecrets the cache push is the
	//    prereq; the render-time lookup already verified the cache had
	//    a CA matching the domain.
	if !opts.SkipPrereqCheck && !opts.SyncLocalSecrets {
		step := opts.Progress.Start(fmt.Sprintf("check prerequisite Secret %s", prereq.CustomCASecretName))
		if err := prereq.CheckCustomCASecret(ctx, opts.Getter, OperatorNamespace); err != nil {
			step.Fail(err)
			return err
		}
		step.Done("present")
	}

	// 5. SecretsManager + wait.
	if err := applyAndWaitStep(ctx, opts, applier, waiter, out.SecretsManager, "SecretsManager"); err != nil {
		return err
	}

	// 6. LookupService + SessionManager — applied together, waited
	//    together. See remote-access-token cycle comment from commit
	//    0d79afc6.
	if out.LookupService != nil {
		if err := applyOnlyStep(ctx, opts, applier, out.LookupService, "LookupService"); err != nil {
			return err
		}
	}
	if err := applyOnlyStep(ctx, opts, applier, out.SessionManager, "SessionManager"); err != nil {
		return err
	}
	if out.LookupService != nil {
		if err := waitOnlyStep(ctx, opts, waiter, out.LookupService, "LookupService"); err != nil {
			return err
		}
	}
	if err := waitOnlyStep(ctx, opts, waiter, out.SessionManager, "SessionManager"); err != nil {
		return err
	}

	opts.Progress.Note("deploy complete")
	return nil
}

// applyAndWaitStep does apply + wait under a single progress step.
// Used by ECC and SecretsManager where the two operations are
// strictly sequential.
func applyAndWaitStep(ctx context.Context, opts Options, applier *apply.Client, waiter *wait.Client, obj map[string]interface{}, label string) error {
	if err := applyOnlyStep(ctx, opts, applier, obj, label); err != nil {
		return err
	}
	return waitOnlyStep(ctx, opts, waiter, obj, label)
}

// applyOnlyStep is one progress step that just applies and reports
// the apply outcome. Used by the interleaved LookupService /
// SessionManager path where applies and waits are deliberately split.
func applyOnlyStep(ctx context.Context, opts Options, applier *apply.Client, obj map[string]interface{}, label string) error {
	u, err := mapToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	step := opts.Progress.Start(fmt.Sprintf("apply %s/%s", label, u.GetName()))
	if _, err := applier.Apply(ctx, u); err != nil {
		step.Fail(err)
		return err
	}
	step.Done("applied")
	return nil
}

// waitOnlyStep is one progress step that just polls for Ready and
// surfaces phase changes (when the CR's status.phase field updates)
// as Update calls on the step.
func waitOnlyStep(ctx context.Context, opts Options, waiter *wait.Client, obj map[string]interface{}, label string) error {
	u, err := mapToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	step := opts.Progress.Start(fmt.Sprintf("wait %s/%s Ready", label, u.GetName()))
	if _, err := waiter.WaitReadyWithPhase(ctx, u.GroupVersionKind(), u.GetNamespace(), u.GetName(), opts.Timeout, step.Update); err != nil {
		step.Fail(err)
		return err
	}
	step.Done("Ready")
	return nil
}

// syncLocalSecrets copies <data-home>/secrets/*.yaml into the cluster's
// 'educates-secrets' namespace. Reuses the v3 secrets package so the
// laptop flow stays the same; deletion of the v3 package in step 9 will
// fold this through whatever the new home is.
func syncLocalSecrets(getter genericclioptions.RESTClientGetter) error {
	cfg, err := getter.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("REST config for secrets sync: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("kubernetes client for secrets sync: %w", err)
	}
	if err := secrets.SyncLocalCachedSecretsToCluster(cs); err != nil {
		return fmt.Errorf("sync local secrets: %w", err)
	}
	return nil
}

// mapToUnstructured roundtrips a translator-produced CR map through JSON
// to get an *unstructured.Unstructured. The translator's maps are already
// JSON-shaped (string keys, no yaml-v2 interface{} maps) since they're
// built with map[string]interface{} literals.
func mapToUnstructured(m map[string]interface{}) (*unstructured.Unstructured, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	u := &unstructured.Unstructured{}
	if err := u.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if u.GroupVersionKind() == (schema.GroupVersionKind{}) {
		return nil, fmt.Errorf("missing apiVersion/kind")
	}
	return u, nil
}
