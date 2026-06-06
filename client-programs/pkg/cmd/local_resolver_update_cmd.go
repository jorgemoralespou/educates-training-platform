package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
)

type LocalResolverUpdateOptions struct {
	Config      string
	LocalConfig bool
}

func (o *LocalResolverUpdateOptions) Run() error {
	cfg, err := loadResolverInputs(o.Config, o.LocalConfig)
	if err != nil {
		return err
	}
	return resolver.UpdateResolver(cfg.Ingress.Domain, cfg.Resolver.TargetAddress, cfg.Resolver.ExtraDomains)
}

func (p *ProjectInfo) NewLocalResolverUpdateCmd() *cobra.Command {
	var o LocalResolverUpdateOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "update",
		Short: "Updates the local DNS resolver (macOS)",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
	}
	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind)")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false, "use <data-home>/config.yaml")
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	c.MarkFlagsOneRequired("config", "local-config")
	return c
}
