package cmd

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localMirrorListExample = `
  # List the deployed local image registry mirrors:
  educates local mirror list
`

func (p *ProjectInfo) NewLocalMirrorListCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "list",
		Short:   "Lists the local image registry mirrors",
		Example: localMirrorListExample,
		RunE: func(_ *cobra.Command, _ []string) error {
			mirrors, err := registry.ListRegistryMirrors()
			if err != nil {
				return errors.Wrap(err, "failed to list registry mirrors")
			}

			if len(mirrors) == 0 {
				fmt.Println("No mirrors found.")
				return nil
			}

			rows := make([][]string, 0, len(mirrors))
			for _, m := range mirrors {
				rows = append(rows, []string{m.Name, m.URL, m.Username, m.Status, m.ContainerName})
			}

			fmt.Println(utils.PrintTable([]string{"NAME", "URL", "USERNAME", "STATUS", "CONTAINER_NAME"}, rows))

			return nil
		},
	}

	return c
}
