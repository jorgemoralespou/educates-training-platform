package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
)

var localResolverDeleteExample = `
  # Delete the local DNS resolver:
  educates local resolver delete
`

func (p *ProjectInfo) NewLocalResolverDeleteCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "delete",
		Short:   "Deletes the local DNS resolver",
		Example: localResolverDeleteExample,
		RunE:    func(_ *cobra.Command, _ []string) error { return resolver.DeleteResolver() },
	}

	return c
}
