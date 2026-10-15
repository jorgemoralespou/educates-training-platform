package cmd

import (
	"fmt"
	"io"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localSecretsExportExample = `
  # Export all cached secrets as YAML:
  educates local secrets export

  # Export a single cached secret as YAML:
  educates local secrets export mydomain-tls

  # Export a CA certificate as PEM for importing into a trust store:
  educates local secrets export mydomain-ca --pem > mydomain-ca.pem
`

func (p *ProjectInfo) NewLocalSecretsExportCmd() *cobra.Command {
	var pem bool

	var c = &cobra.Command{
		Args:    cobra.ArbitraryArgs,
		Use:     "export [NAME]",
		Short:   "Export secrets in the cache",
		Example: localSecretsExportExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			if pem {
				if len(args) != 1 {
					return utils.CmdError(cmd, "--pem requires exactly one secret name", "NAME")
				}

				return printSecretCertificatePEM(cmd.OutOrStdout(), args[0])
			}

			secretsCacheDir, err := secrets.CacheDir()
			if err != nil {
				return err
			}

			if err := utils.PrintYamlFilesInDir(secretsCacheDir, args); err != nil {
				return errors.Wrapf(err, "unable to read secrets cache directory")
			}

			return nil
		},
	}

	c.Flags().BoolVar(
		&pem,
		"pem",
		false,
		"print the secret's certificate as PEM (for importing a CA into a trust store)",
	)

	return c
}

// printSecretCertificatePEM writes the certificate of a cached TLS/CA
// secret to w as PEM, so it can be redirected to a file and imported
// into an operating system trust store: tls.crt, or ca.crt for a CA
// trust Secret. The private key is never printed.
func printSecretCertificatePEM(w io.Writer, name string) error {
	secret, err := secrets.LoadCachedSecret(name)
	if err != nil {
		return err
	}

	certificate := secret.Data["tls.crt"]
	if len(certificate) == 0 {
		certificate = secret.Data["ca.crt"]
	}

	if len(certificate) == 0 {
		return errors.Errorf("secret %q does not contain a certificate", name)
	}

	_, err = fmt.Fprint(w, string(certificate))

	return err
}
