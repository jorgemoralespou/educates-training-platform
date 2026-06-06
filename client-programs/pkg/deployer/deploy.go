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
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/helm"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/prereq"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/wait"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
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

	// 1. Helm install/upgrade the operator chart.
	fmt.Fprintln(opts.Out, "→ helm upgrade --install", OperatorReleaseName)
	chrt, err := chart.Load()
	if err != nil {
		return fmt.Errorf("load embedded chart: %w", err)
	}
	helmClient, err := helm.New(opts.Getter, OperatorNamespace, opts.HelmLog)
	if err != nil {
		return err
	}
	if _, err := helmClient.UpgradeOrInstall(ctx, OperatorReleaseName, chrt, out.OperatorChartValues); err != nil {
		return err
	}
	fmt.Fprintln(opts.Out, "  ✓ helm release installed")

	// 2. Apply EducatesClusterConfig + wait.
	applier, err := apply.New(opts.Getter)
	if err != nil {
		return err
	}
	waiter, err := wait.New(opts.Getter)
	if err != nil {
		return err
	}

	if err := applyAndWait(ctx, opts, applier, waiter,
		out.EducatesClusterConfig, "EducatesClusterConfig"); err != nil {
		return err
	}

	// 3. Prereq check — the SecretsManager controller will error without
	// the Secret in place, so fail fast with a friendly message rather
	// than letting it surface as a Ready=False with controller-internal
	// reason text.
	if !opts.SkipPrereqCheck {
		fmt.Fprintln(opts.Out, "→ checking prerequisite Secret", prereq.CustomCASecretName)
		if err := prereq.CheckCustomCASecret(ctx, opts.Getter, OperatorNamespace); err != nil {
			return err
		}
		fmt.Fprintln(opts.Out, "  ✓ prerequisite present")
	}

	// 4. SecretsManager + wait.
	if err := applyAndWait(ctx, opts, applier, waiter,
		out.SecretsManager, "SecretsManager"); err != nil {
		return err
	}

	// 5. LookupService (if present).
	if out.LookupService != nil {
		if err := applyAndWait(ctx, opts, applier, waiter,
			out.LookupService, "LookupService"); err != nil {
			return err
		}
	}

	// 6. SessionManager.
	if err := applyAndWait(ctx, opts, applier, waiter,
		out.SessionManager, "SessionManager"); err != nil {
		return err
	}

	fmt.Fprintln(opts.Out, "✓ deploy complete")
	return nil
}

func applyAndWait(ctx context.Context, opts Options, applier *apply.Client, waiter *wait.Client, obj map[string]interface{}, label string) error {
	u, err := mapToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	fmt.Fprintf(opts.Out, "→ apply %s/%s\n", label, u.GetName())
	if _, err := applier.Apply(ctx, u); err != nil {
		return err
	}
	fmt.Fprintf(opts.Out, "→ wait %s/%s Ready=True (timeout %s)\n", label, u.GetName(), opts.Timeout)
	if _, err := waiter.WaitReady(ctx, u.GroupVersionKind(), u.GetNamespace(), u.GetName(), opts.Timeout); err != nil {
		return err
	}
	fmt.Fprintf(opts.Out, "  ✓ %s/%s Ready\n", label, u.GetName())
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
