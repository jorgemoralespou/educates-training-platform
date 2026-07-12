package cmd

import (
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

var localSecretsAddTlsExample = `
  # Cache a TLS certificate for a wildcard ingress domain:
  educates local secrets add tls mydomain-tls --cert tls.crt --key tls.key --domain mydomain.test
`

type LocalSecretsAddTlsOptions struct {
	CertFile      string
	KeyFile       string
	IngressDomain string
}

func (o *LocalSecretsAddTlsOptions) Run(name string) error {
	var err error
	var certificateFileData []byte
	var certificateKeyFileData []byte

	if o.CertFile != "" {
		certificateFileData, err = os.ReadFile(o.CertFile)

		if err != nil {
			return errors.Wrapf(err, "failed to read certificate file %s", o.CertFile)
		}
	}

	if o.KeyFile != "" {
		certificateKeyFileData, err = os.ReadFile(o.KeyFile)

		if err != nil {
			return errors.Wrapf(err, "failed to read certificate key file %s", o.KeyFile)
		}
	}

	secret := secrets.NewTLSSecret(name, certificateFileData, certificateKeyFileData, o.IngressDomain)

	return secrets.WriteCachedSecret(secret)
}

func (p *ProjectInfo) NewLocalSecretsAddTlsCmd() *cobra.Command {
	var o LocalSecretsAddTlsOptions

	var c = &cobra.Command{
		Args:    secretNameArgs,
		Use:     "tls NAME",
		Short:   "Create a TLS secret",
		Example: localSecretsAddTlsExample,
		RunE:    func(_ *cobra.Command, args []string) error { return o.Run(args[0]) },
	}

	c.Flags().StringVar(
		&o.CertFile,
		"cert",
		"",
		"path to PEM encoded public key certificate",
	)
	c.Flags().StringVar(
		&o.KeyFile,
		"key",
		"",
		"path to private key associated with given certificate",
	)
	c.Flags().StringVar(
		&o.IngressDomain,
		"domain",
		"",
		"wildcard ingress domain matching certificate",
	)

	c.MarkFlagsRequiredTogether("cert", "key")

	return c
}
