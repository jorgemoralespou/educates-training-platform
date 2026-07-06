package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/templates"
)

var templateListExample = `
  # List the available workshop templates:
  educates template list
`

func (p *ProjectInfo) NewTemplateListCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "list",
		Short:   "List available workshop templates",
		Example: templateListExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			for _, name := range templates.ListWorkshopTemplates() {
				cmd.Println(name)
			}

			return nil
		},
	}

	return c
}
