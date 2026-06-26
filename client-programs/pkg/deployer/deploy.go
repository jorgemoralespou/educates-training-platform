// Package deployer is the v4 install path: load a CLI config, translate
// to operator chart values + four platform CRs, install the operator
// chart via Helm SDK, server-side-apply the CRs in dependency order, and
// wait for each to be Ready=True.
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

	// Push cached local secrets so the operator can consume them.
	if opts.SyncLocalSecrets {
		step := opts.Progress.Start("syncing cached local secrets to cluster")
		if err := syncLocalSecrets(opts.Getter); err != nil {
			step.Fail(err)
			return err
		}
		step.Done("")
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
	step := opts.Progress.Start("Installing Educates Kubernetes Operator")
	helmClient, err := helm.New(opts.Getter, OperatorNamespace, opts.HelmLog)
	if err != nil {
		step.Fail(err)
		return err
	}
	if _, err := helmClient.UpgradeOrInstall(ctx, OperatorReleaseName, chrt, out.OperatorChartValues); err != nil {
		step.Fail(err)
		return err
	}
	step.Done("")

	// 3. Apply EducatesClusterConfig + wait.
	if err := installStep(ctx, opts, applier, waiter, out.EducatesClusterConfig, "EducatesClusterConfig"); err != nil {
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
	if err := installStep(ctx, opts, applier, waiter, out.SecretsManager, "SecretsManager"); err != nil {
		return err
	}

	// 6. LookupService + SessionManager — applied together, waited
	//    together. See remote-access-token cycle comment from commit
	//    0d79afc6. Both applies happen before either wait; the per-CR
	//    "Installing" step covers the wait (the slow, meaningful part).
	if out.LookupService != nil {
		if err := applyQuiet(ctx, applier, out.LookupService, "LookupService"); err != nil {
			return err
		}
	}
	if err := applyQuiet(ctx, applier, out.SessionManager, "SessionManager"); err != nil {
		return err
	}
	if out.LookupService != nil {
		if err := installWaitStep(ctx, opts, waiter, out.LookupService, "LookupService"); err != nil {
			return err
		}
	}
	if err := installWaitStep(ctx, opts, waiter, out.SessionManager, "SessionManager"); err != nil {
		return err
	}

	opts.Progress.Note("Educates successfully deployed")
	return nil
}

// installStep applies obj and waits for it to become Ready under a
// single "Installing <label>" progress step. The apply is surfaced as an
// "applying" phase and the wait surfaces the CR's status.phase changes,
// so one morphing line carries the whole install of one component. Used
// by ECC and SecretsManager, where apply and wait are strictly
// sequential.
func installStep(ctx context.Context, opts Options, applier *apply.Client, waiter *wait.Client, obj map[string]interface{}, label string) error {
	u, err := mapToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	step := opts.Progress.Start("Installing " + label)
	step.Update("applying")
	if _, err := applier.Apply(ctx, u); err != nil {
		step.Fail(err)
		return err
	}
	if _, err := waiter.WaitReadyWithPhase(ctx, u.GroupVersionKind(), u.GetNamespace(), u.GetName(), opts.Timeout, step.Update); err != nil {
		step.Fail(err)
		return err
	}
	step.Done("")
	return nil
}

// installWaitStep waits for an already-applied obj to become Ready under
// a single "Installing <label>" step. Used by the LookupService /
// SessionManager pair, whose applies are both done up front (see the
// remote-access-token cycle) before either is waited on.
func installWaitStep(ctx context.Context, opts Options, waiter *wait.Client, obj map[string]interface{}, label string) error {
	u, err := mapToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	step := opts.Progress.Start("Installing " + label)
	if _, err := waiter.WaitReadyWithPhase(ctx, u.GroupVersionKind(), u.GetNamespace(), u.GetName(), opts.Timeout, step.Update); err != nil {
		step.Fail(err)
		return err
	}
	step.Done("")
	return nil
}

// applyQuiet applies obj without opening a progress step. Used for the
// LookupService / SessionManager pair, which must both be applied before
// either is waited on; their "Installing" steps cover the wait.
func applyQuiet(ctx context.Context, applier *apply.Client, obj map[string]interface{}, label string) error {
	u, err := mapToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if _, err := applier.Apply(ctx, u); err != nil {
		return fmt.Errorf("apply %s: %w", label, err)
	}
	return nil
}

// syncLocalSecrets copies <data-home>/secrets/*.yaml into the cluster's
// 'educates-secrets' namespace. Reuses the secrets package so the
// laptop flow stays the same.
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
