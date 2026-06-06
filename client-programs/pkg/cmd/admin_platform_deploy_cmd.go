package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// PlatformDeployOptions mirrors PlatformRenderOptions plus the kubectl
// connection flags consumed by the install path.
type PlatformDeployOptions struct {
	Config      string
	LocalConfig bool
	Kubeconfig  string
	Context     string
	Timeout     time.Duration
	Verbose     bool
}

func (p *ProjectInfo) NewAdminPlatformDeployCmd() *cobra.Command {
	var o PlatformDeployOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "deploy",
		Short: "Install Educates: helm install operator + apply 4 platform CRs",
		Long: `Drive the install end-to-end. Same translator as 'admin platform render',
then push to the cluster:

  1. helm upgrade --install educates-installer (embedded chart)
  2. apply EducatesClusterConfig → wait Ready=True
  3. verify educates-custom-ca Secret prerequisite (skipped when
     --local-config syncs the cached CA in step 0)
  4. apply SecretsManager → wait Ready=True
  5. apply LookupService (if configured) → wait Ready=True
     (interleaved with SessionManager — both apply, then both wait)
  6. apply SessionManager → wait Ready=True`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return p.runDeploy(cmd.Context(), cmd.OutOrStdout(), &o)
		},
	}

	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind)")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false,
		"use <data-home>/config.yaml; applies host-IP nip.io fallback for ingress.domain")
	c.Flags().StringVar(&o.Kubeconfig, "kubeconfig", "", "kubeconfig file (defaults to $KUBECONFIG / ~/.kube/config)")
	c.Flags().StringVar(&o.Context, "context", "", "context name to use within the kubeconfig")
	c.Flags().DurationVar(&o.Timeout, "timeout", deployer.DefaultTimeout, "per-CR Ready=True wait timeout")
	c.Flags().BoolVar(&o.Verbose, "verbose", false, "show helm SDK debug output on stderr")
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	c.MarkFlagsOneRequired("config", "local-config")

	return c
}

func (p *ProjectInfo) runDeploy(ctx context.Context, w io.Writer, o *PlatformDeployOptions) error {
	// Reuse the same load → default → translate path as render so the
	// two commands stay in lock-step. (Step-9 cleanup factors this into
	// a shared helper.)
	path, err := resolveDeployConfigPath(o)
	if err != nil {
		return err
	}
	if o.LocalConfig {
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			return config.MissingLocalConfigError(utils.GetEducatesHomeDir())
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	opts := translator.Options{}
	syncLocalSecrets := false
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		c.ApplyCLIDefaults(p.Version, p.ImageRepository)
		if o.LocalConfig && c.Ingress.Domain == "" {
			ip, err := hostinfo.DetectHostIP()
			if err != nil {
				return fmt.Errorf("auto-detect host IP: %w", err)
			}
			c.Ingress.Domain = hostinfo.NipDomain(ip)
		} else if c.Ingress.Domain == "" {
			return fmt.Errorf("ingress.domain is required when using --config (set it in %s)", path)
		}
		caName, lookupErr := lookupLocalCAByDomain(c.Ingress.Domain)
		if lookupErr != nil {
			return lookupErr
		}
		opts.CASecretName = caName
		opts.CASecretNamespace = LocalCASecretNamespace
		syncLocalSecrets = true
	case *v1alpha1.EducatesConfig:
		// Pure passthrough.
	}

	out, err := translator.Translate(cfg, opts)
	if err != nil {
		return err
	}

	// Build the kubectl-style RESTClientGetter from the connection flags.
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

	return deployer.Deploy(ctx, out, deployer.Options{
		Getter:           cf,
		Out:              w,
		HelmLog:          helmLog,
		Timeout:          o.Timeout,
		SyncLocalSecrets: syncLocalSecrets,
	})
}

func resolveDeployConfigPath(o *PlatformDeployOptions) (string, error) {
	if o.LocalConfig {
		return filepath.Join(utils.GetEducatesHomeDir(), "config.yaml"), nil
	}
	if o.Config == "" {
		return "", fmt.Errorf("internal: neither --config nor --local-config set")
	}
	return o.Config, nil
}
