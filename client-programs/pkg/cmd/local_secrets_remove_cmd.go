package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

var localSecretsRemoveExample = `
  # Remove a secret from the local cache:
  educates local secrets remove mydomain-tls
`

func (p *ProjectInfo) NewLocalSecretsRemoveCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:    secretNameArgs,
		Use:     "remove NAME",
		Short:   "Remove secret from the cache",
		Example: localSecretsRemoveExample,
		RunE: func(_ *cobra.Command, args []string) error {
			return secrets.RemoveCachedSecret(args[0])
		},
	}

	return c
}
