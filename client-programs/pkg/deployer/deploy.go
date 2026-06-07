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

	// 0. Push cached local secrets (CA + TLS) into the cluster. For the
	//    laptop flow this is the source of the Secret that the
	//    operator's CustomCA path will mirror into cert-manager's
	//    namespace; ECC.caCertificateRef points at 'educates-secrets'.
	if opts.SyncLocalSecrets {
		fmt.Fprintln(opts.Out, "→ syncing cached local secrets to cluster")
		if err := syncLocalSecrets(opts.Getter); err != nil {
			return err
		}
		fmt.Fprintln(opts.Out, "  ✓ secrets synced")
	}

	chrt, err := chart.Load()
	if err != nil {
		return fmt.Errorf("load embedded chart: %w", err)
	}

	// Applier needs to exist before the CRD step (which uses it) so
	// hoist its construction before helm.
	applier, err := apply.New(opts.Getter)
	if err != nil {
		return err
	}
	waiter, err := wait.New(opts.Getter)
	if err != nil {
		return err
	}

	// 1. CRDs. helm only installs CRDs from a chart's crds/ directory
	// on first install (never on upgrade), so a CRD shape change
	// would leave the cluster on the old schema. Owning CRD lifecycle
	// here means re-deploys always have the latest. SkipCRDApply
	// opts out for users managing CRDs out of band.
	if !opts.SkipCRDApply {
		fmt.Fprintln(opts.Out, "→ apply CRDs")
		applied, err := crds.Apply(ctx, applier, chrt)
		if err != nil {
			return err
		}
		fmt.Fprintf(opts.Out, "  ✓ %d CRDs applied\n", len(applied))
	}

	// 2. Helm install/upgrade the operator chart. SkipCRDs=true on the
	// helm side because step 1 already owns the CRD lifecycle.
	fmt.Fprintln(opts.Out, "→ helm upgrade --install", OperatorReleaseName)
	helmClient, err := helm.New(opts.Getter, OperatorNamespace, opts.HelmLog)
	if err != nil {
		return err
	}
	if _, err := helmClient.UpgradeOrInstall(ctx, OperatorReleaseName, chrt, out.OperatorChartValues); err != nil {
		return err
	}
	fmt.Fprintln(opts.Out, "  ✓ helm release installed")

	// 3. Apply EducatesClusterConfig + wait.

	if err := applyAndWait(ctx, opts, applier, waiter,
		out.EducatesClusterConfig, "EducatesClusterConfig"); err != nil {
		return err
	}

	// 3. Prereq check — only meaningful when the caller didn't sync
	//    local secrets. With SyncLocalSecrets the cache push is the
	//    prereq; the render-time lookup already verified the cache had
	//    a CA matching the domain.
	if !opts.SkipPrereqCheck && !opts.SyncLocalSecrets {
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

	// 5. LookupService + SessionManager — applied together, waited
	//    together. SessionManager's remote-access subchart installs in
	//    Auto mode when a LookupService CR exists in the cluster, and
	//    produces the 'remote-access-token' Secret that LookupService's
	//    pod mounts. Applying LookupService first and waiting for
	//    Ready would deadlock (token never appears); applying
	//    SessionManager first would skip the remote-access install
	//    (no LookupService CR yet).
	if out.LookupService != nil {
		if err := apply_(ctx, opts, applier, out.LookupService, "LookupService"); err != nil {
			return err
		}
	}
	if err := apply_(ctx, opts, applier, out.SessionManager, "SessionManager"); err != nil {
		return err
	}
	if out.LookupService != nil {
		if err := wait_(ctx, opts, waiter, out.LookupService, "LookupService"); err != nil {
			return err
		}
	}
	if err := wait_(ctx, opts, waiter, out.SessionManager, "SessionManager"); err != nil {
		return err
	}

	fmt.Fprintln(opts.Out, "✓ deploy complete")
	return nil
}

func applyAndWait(ctx context.Context, opts Options, applier *apply.Client, waiter *wait.Client, obj map[string]interface{}, label string) error {
	if err := apply_(ctx, opts, applier, obj, label); err != nil {
		return err
	}
	return wait_(ctx, opts, waiter, obj, label)
}

func apply_(ctx context.Context, opts Options, applier *apply.Client, obj map[string]interface{}, label string) error {
	u, err := mapToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	fmt.Fprintf(opts.Out, "→ apply %s/%s\n", label, u.GetName())
	if _, err := applier.Apply(ctx, u); err != nil {
		return err
	}
	return nil
}

func wait_(ctx context.Context, opts Options, waiter *wait.Client, obj map[string]interface{}, label string) error {
	u, err := mapToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	fmt.Fprintf(opts.Out, "→ wait %s/%s Ready=True (timeout %s)\n", label, u.GetName(), opts.Timeout)
	if _, err := waiter.WaitReady(ctx, u.GroupVersionKind(), u.GetNamespace(), u.GetName(), opts.Timeout); err != nil {
		return err
	}
	fmt.Fprintf(opts.Out, "  ✓ %s/%s Ready\n", label, u.GetName())
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
