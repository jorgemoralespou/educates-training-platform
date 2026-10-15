package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

var localSecretsRemoveExample = `
  # Remove a secret from the local cache:
  educates local secrets remove mydomain-tls

  # Remove the root CA, with its private key and every CA minted from it:
  educates local secrets remove educates-ca
`

type LocalSecretsRemoveOptions struct{}

// Run removes the secret name from the local secrets cache, printing a
// line for each cached secret it removes. Removing a root CA removes its
// CA trust Secret, its CA root key pair and every CA key pair minted
// from it.
func (o *LocalSecretsRemoveOptions) Run(w io.Writer, name string) error {
	removed, err := secrets.RemoveCachedSecretAndDependents(name)
	for _, r := range removed {
		fmt.Fprintf(w, "Removed %q from the local secrets cache.\n", r)
	}
	return err
}

func (p *ProjectInfo) NewLocalSecretsRemoveCmd() *cobra.Command {
	var o LocalSecretsRemoveOptions

	var c = &cobra.Command{
		Args:  secretNameArgs,
		Use:   "remove NAME",
		Short: "Remove secret from the cache",
		Long: `Remove a secret from the local secrets cache.

Removing a root CA, by the name of its CA certificate NAME or of its CA
root key pair NAME-root-key-pair, removes both and every CA key pair
minted from the root, so no CA material of that root stays in the cache.`,
		Example: localSecretsRemoveExample,
		RunE:    func(cmd *cobra.Command, args []string) error { return o.Run(cmd.OutOrStdout(), args[0]) },
	}

	return c
}
