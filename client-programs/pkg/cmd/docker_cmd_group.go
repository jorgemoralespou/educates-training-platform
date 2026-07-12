package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
)

func (p *ProjectInfo) NewDockerCmdGroup() *cobra.Command {
	var c = &cobra.Command{
		Use:   "docker",
		Short: "Tools for deploying workshops to Docker",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return docker.CheckDaemonRunning()
		},
	}

	// Use a command group as it allows us to dictate the order in which they
	// are displayed in the help message, as otherwise they are displayed in
	// sort order.

	commandGroups := templates.CommandGroups{
		{
			Message: "Available Commands:",
			Commands: []*cobra.Command{
				p.NewDockerWorkshopCmdGroup(),
				p.NewDockerExtensionCmdGroup(),
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	return c
}
