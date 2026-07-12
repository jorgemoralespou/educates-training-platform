package cmd

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
)

var localRegistryDeleteExample = `
  # Delete the local image registry:
  educates local registry delete
`

type LocalRegistryDeleteOptions struct {
}

func (o *LocalRegistryDeleteOptions) Run() error {
	err := stepOnStdout(false, "delete local registry", "deleted", func(s progress.Step) error {
		return registry.DeleteRegistry(s)
	})

	if err != nil {
		return errors.Wrap(err, "failed to delete registry")
	}

	return nil
}

func (p *ProjectInfo) NewLocalRegistryDeleteCmd() *cobra.Command {
	var o LocalRegistryDeleteOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "delete",
		Short:   "Deletes the local image registry",
		Example: localRegistryDeleteExample,
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run() },
	}

	return c
}
