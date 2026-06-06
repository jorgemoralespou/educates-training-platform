package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

type LocalResolverDeployOptions struct {
	Config      string
	LocalConfig bool
	Domain      string
}

func (o *LocalResolverDeployOptions) Run() error {
	cfg, err := loadResolverInputs(o.Config, o.LocalConfig)
	if err != nil {
		return err
	}
	domain := cfg.Ingress.Domain
	if o.Domain != "" {
		domain = o.Domain
	}
	return resolver.DeployResolver(domain, cfg.Resolver.TargetAddress, cfg.Resolver.ExtraDomains)
}

func (p *ProjectInfo) NewLocalResolverDeployCmd() *cobra.Command {
	var o LocalResolverDeployOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "deploy",
		Short: "Deploys a local DNS resolver (macOS)",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
	}
	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind)")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false, "use <data-home>/config.yaml")
	c.Flags().StringVar(&o.Domain, "domain", "", "override ingress.domain from the config")
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	c.MarkFlagsOneRequired("config", "local-config")
	return c
}

// loadResolverInputs loads an EducatesLocalConfig from --config or
// --local-config and returns the parts the resolver helpers need.
// EducatesConfig (escape hatch) is accepted when target.cluster /
// target.resolver are populated.
func loadResolverInputs(configPath string, useLocalConfig bool) (*v1alpha1.EducatesLocalConfig, error) {
	var path string
	if useLocalConfig {
		path = filepath.Join(utils.GetEducatesHomeDir(), "config.yaml")
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			return nil, config.MissingLocalConfigError(utils.GetEducatesHomeDir())
		}
	} else {
		path = configPath
	}
	loaded, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	switch c := loaded.(type) {
	case *v1alpha1.EducatesLocalConfig:
		return c, nil
	case *v1alpha1.EducatesConfig:
		if c.Target == nil {
			return nil, fmt.Errorf("%s: EducatesConfig has no target block; resolver needs target.resolver.*", path)
		}
		return &v1alpha1.EducatesLocalConfig{
			TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.APIVersion, Kind: v1alpha1.KindEducatesLocalConfig},
			Cluster:  c.Target.Cluster,
			Resolver: c.Target.Resolver,
		}, nil
	default:
		return nil, fmt.Errorf("%s: unsupported kind %q for resolver commands", path, loaded.GetKind())
	}
}
