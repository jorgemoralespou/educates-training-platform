package cmd

import (
	"context"
	"io"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
)

type PlatformDeleteOptions struct {
	Kubeconfig string
	Context    string
	Timeout    time.Duration
	Verbose    bool
}

func (p *ProjectInfo) NewAdminPlatformDeleteCmd() *cobra.Command {
	var o PlatformDeleteOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "delete",
		Short: "Uninstall Educates: drain platform CRs + helm uninstall",
		Long: `Reverse of 'admin platform deploy':

  1. delete SessionManager → wait gone
  2. delete LookupService → wait gone
  3. delete SecretsManager → wait gone
  4. delete EducatesClusterConfig → wait gone
       (the ECC finalizer drains kyverno, external-dns, contour,
        cert-manager and the CustomCA Secret copy in reverse install order)
  5. helm uninstall the operator chart

Idempotent: missing CRs are skipped silently. Does NOT delete the CRDs,
the operator namespace, the educates-secrets namespace, or any
locally-cached secrets — those are user state preserved across reinstalls.

Unlike deploy, this command takes no --config / --local-config — the
resources are always the four CRs at metadata.name=cluster plus the
educates-installer release. The kubeconfig flags suffice.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return p.runDelete(cmd.Context(), cmd.OutOrStdout(), &o)
		},
	}

	c.Flags().StringVar(&o.Kubeconfig, "kubeconfig", "", "kubeconfig file (defaults to $KUBECONFIG / ~/.kube/config)")
	c.Flags().StringVar(&o.Context, "context", "", "context name to use within the kubeconfig")
	c.Flags().DurationVar(&o.Timeout, "timeout", deployer.DefaultTimeout, "per-CR finalizer-drain wait timeout")
	c.Flags().BoolVar(&o.Verbose, "verbose", false, "show helm SDK debug output on stderr")

	return c
}

func (p *ProjectInfo) runDelete(ctx context.Context, w io.Writer, o *PlatformDeleteOptions) error {
	cf := genericclioptions.NewConfigFlags(true)
	if o.Kubeconfig != "" {
		cf.KubeConfig = &o.Kubeconfig
	}
	if o.Context != "" {
		cf.Context = &o.Context
	}
	ns := deployer.OperatorNamespace
	cf.Namespace = &ns

	helmLog := io.Discard
	if o.Verbose {
		helmLog = w
	}

	return deployer.Delete(ctx, deployer.DeleteOptions{
		Getter:  cf,
		Out:     w,
		HelmLog: helmLog,
		Timeout: o.Timeout,
	})
}
