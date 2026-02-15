package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"
)

/*
Create Cobra command group for commands related to workshop bundles.
*/
func (p *ProjectInfo) NewBundleCmdGroup() *cobra.Command {
	var c = &cobra.Command{
		Use:     "bundle",
		Aliases: []string{"bundles"},
		Short:   "Tools for working with multi-workshop bundles",
	}

	commandGroups := templates.CommandGroups{
		{
			Message: "Available Commands:",
			Commands: []*cobra.Command{
				p.NewBundleNewCmd(),
				p.NewBundlePublishCmd(),
				p.NewBundleExportCmd(),
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	return c
}
