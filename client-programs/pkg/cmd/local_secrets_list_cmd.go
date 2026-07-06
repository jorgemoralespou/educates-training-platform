package cmd

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

var localSecretsListExample = `
  # List the names of all secrets in the local cache:
  educates local secrets list
`

func (p *ProjectInfo) NewLocalSecretsListCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "list",
		Short:   "List secrets in the cache",
		Example: localSecretsListExample,
		RunE: func(_ *cobra.Command, _ []string) error {
			names, err := secrets.ListCachedSecretNames()
			if err != nil {
				return errors.Wrap(err, "unable to list cached secrets")
			}

			for _, name := range names {
				fmt.Println(name)
			}

			return nil
		},
	}

	return c
}
