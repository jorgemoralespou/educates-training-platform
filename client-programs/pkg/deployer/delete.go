package deployer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/apply"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/helm"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/wait"
)

// DeleteOptions configures a delete run. Mirrors Options but the inputs
// differ: we don't need translator output here since the resources are
// always the same four CRs at metadata.name=cluster + the helm release.
type DeleteOptions struct {
	Getter   genericclioptions.RESTClientGetter
	Out      io.Writer
	HelmLog  io.Writer
	Timeout  time.Duration
	Progress progress.Reporter
}

// Delete executes the uninstall pipeline:
//
//  1. Delete SessionManager → wait gone.
//  2. Delete LookupService → wait gone.
//  3. Delete SecretsManager → wait gone.
//  4. Delete EducatesClusterConfig → wait gone (its finalizer drains
//     kyverno → external-dns → contour → cert-manager → CustomCA
//     copy in reverse install order).
//  5. helm uninstall the operator chart.
//
// Idempotent: a CR that's already gone is a no-op step. Useful for
// re-running after a half-finished delete or for cleaning up state the
// last deploy never created.
//
// Deliberately NOT deleted (user state across reinstalls):
//   - CRDs (helm never owns them; would cascade-delete any CR in the
//     cluster, including ones from other Educates installs).
//   - The operator namespace itself.
//   - educates-secrets namespace + the synced CA Secret (so the next
//     deploy reuses the same CA the user already trusts in their
//     browser).
func Delete(ctx context.Context, opts DeleteOptions) error {
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

	applier, err := apply.New(opts.Getter)
	if err != nil {
		return err
	}
	waiter, err := wait.New(opts.Getter)
	if err != nil {
		return err
	}

	for _, st := range deleteOrder() {
		if err := deleteCRStep(ctx, opts, applier, waiter, st.gvk, st.name, st.label); err != nil {
			return err
		}
	}

	// helm uninstall the operator chart.
	step := opts.Progress.Start(fmt.Sprintf("helm uninstall %s", OperatorReleaseName))
	helmClient, err := helm.New(opts.Getter, OperatorNamespace, opts.HelmLog)
	if err != nil {
		step.Fail(err)
		return err
	}
	if err := helmClient.Uninstall(OperatorReleaseName); err != nil {
		// Uninstall already swallows "release not found"; surface other
		// errors as-is.
		step.Fail(err)
		return err
	}
	step.Done("uninstalled")
	opts.Progress.Note("delete complete")
	return nil
}

// deleteCRStep removes one CR by GVK + name and waits for it to be
// 404, reporting both halves through a single progress step. NotFound
// at the delete call → step closes with "already gone" + return nil
// (idempotent re-run).
func deleteCRStep(ctx context.Context, opts DeleteOptions, applier *apply.Client, waiter *wait.Client, gvk schema.GroupVersionKind, name, label string) error {
	step := opts.Progress.Start(fmt.Sprintf("delete %s/%s", label, name))
	if err := applier.Delete(ctx, gvk, "", name); err != nil {
		var statusErr *apierrors.StatusError
		if errors.As(err, &statusErr) && apierrors.IsNotFound(err) {
			step.Done("already gone")
			return nil
		}
		step.Fail(err)
		return err
	}
	step.Update("waiting for finalizer drain")
	if err := waiter.WaitGone(ctx, gvk, "", name, opts.Timeout); err != nil {
		step.Fail(err)
		return err
	}
	step.Done("gone")
	return nil
}

type deleteStep struct {
	gvk   schema.GroupVersionKind
	name  string
	label string
}

// deleteOrder is the inverse of the deploy install order. SessionManager
// first so its remote-access subchart goes away while LookupService is
// still present; LookupService next (its subchart's cleanup runs); then
// SecretsManager; then ECC last (its finalizer drains cluster services).
func deleteOrder() []deleteStep {
	return []deleteStep{
		{
			gvk:   schema.GroupVersionKind{Group: "platform.educates.dev", Version: "v1alpha1", Kind: "SessionManager"},
			name:  "cluster",
			label: "SessionManager",
		},
		{
			gvk:   schema.GroupVersionKind{Group: "platform.educates.dev", Version: "v1alpha1", Kind: "LookupService"},
			name:  "cluster",
			label: "LookupService",
		},
		{
			gvk:   schema.GroupVersionKind{Group: "platform.educates.dev", Version: "v1alpha1", Kind: "SecretsManager"},
			name:  "cluster",
			label: "SecretsManager",
		},
		{
			gvk:   schema.GroupVersionKind{Group: "config.educates.dev", Version: "v1alpha1", Kind: "EducatesClusterConfig"},
			name:  "cluster",
			label: "EducatesClusterConfig",
		},
	}
}
