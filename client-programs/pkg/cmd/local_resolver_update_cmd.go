package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
)

var localResolverUpdateExample = `
  # Update the local DNS resolver from the laptop config:
  educates local resolver update

  # Update using a specific config file:
  educates local resolver update --config config.yaml
`

type LocalResolverUpdateOptions struct {
	Config      string
	LocalConfig bool
}

func (o *LocalResolverUpdateOptions) Run() error {
	cfg, err := loadResolverInputs(o.Config)
	if err != nil {
		return err
	}
	return resolver.UpdateResolver(cfg.Ingress.Domain, cfg.Resolver.TargetAddress, cfg.Resolver.ExtraDomains)
}

func (p *ProjectInfo) NewLocalResolverUpdateCmd() *cobra.Command {
	var o LocalResolverUpdateOptions

	c := &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "update",
		Short:   "Updates the local DNS resolver (macOS)",
		Example: localResolverUpdateExample,
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run() },
	}
	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind)")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false, "use <data-home>/config.yaml (default when --config is not given)")
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	return c
}
