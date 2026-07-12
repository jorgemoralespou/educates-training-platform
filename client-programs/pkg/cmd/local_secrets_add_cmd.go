package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

func (p *ProjectInfo) NewLocalSecretsAddCmdGroup() *cobra.Command {
	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "add",
		Short: "Add secret to the cache",
	}

	// Use a command group as it allows us to dictate the order in which they
	// are displayed in the help message, as otherwise they are displayed in
	// sort order.

	commandGroups := templates.CommandGroups{
		{
			Message: "Available Commands:",
			Commands: []*cobra.Command{
				p.NewLocalSecretsAddCaCmd(),
				p.NewLocalSecretsAddDockerRegistryCmd(),
				// NewLocalSecretsAddGenericCmd(),
				p.NewLocalSecretsAddTlsCmd(),
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	return c
}

// secretNameArgs builds a cobra Args validator that requires exactly one
// NAME argument and validates its shape, returning a meaningful error
// (with the command path and a --help pointer) before any work runs. It is
// shared by every 'local secrets add' subcommand and by 'local secrets
// remove'.
func secretNameArgs(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return utils.CmdError(cmd, "name is required", "NAME")
	}
	if err := secrets.ValidateSecretName(args[0]); err != nil {
		return utils.CmdError(cmd, err.Error(), "NAME")
	}
	return nil
}
