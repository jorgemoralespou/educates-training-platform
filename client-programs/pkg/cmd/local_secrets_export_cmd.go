package cmd

import (
	"crypto/x509"
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

  # Export the root certificate of a CA as PEM for importing into a trust store:
  educates local secrets export educates-ca --pem > educates-ca.pem
`

func (p *ProjectInfo) NewLocalSecretsExportCmd() *cobra.Command {
	var pem bool

	var c = &cobra.Command{
		Args:  cobra.ArbitraryArgs,
		Use:   "export [NAME]",
		Short: "Export secrets in the cache",
		Long: `Export secrets in the local secrets cache.

Without --pem, prints the cached secrets, or the one named, as YAML,
including any private keys they hold.

With --pem, prints only the certificates of the named secret as PEM, and
never a private key: for a CA, its root certificate, and for a TLS
certificate, the certificate and its chain.`,
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
		"print only the certificates of the named secret as PEM, the root certificate for a CA, never a key",
	)

	return c
}

// printSecretCertificatePEM writes the certificates of a cached secret to
// w as PEM, so they can be redirected to a file and imported into an
// operating system trust store. It prints only certificates, re-encoded
// from what it parsed, so no private key or other data is ever printed,
// whatever the secret holds.
//
// What it prints follows the secret's role, as `local secrets list` shows
// it. For a CA certificate it prints ca.crt. For CA material holding a
// key, a CA root key pair, a CA key pair or an unused CA key pair, it
// prints the root certificate: ca.crt when the secret records its root
// there, else tls.crt. For a TLS certificate it prints tls.crt, the
// certificate and its chain. Any other secret holds no certificate to
// print.
func printSecretCertificatePEM(w io.Writer, name string) error {
	secret, err := secrets.LoadCachedSecret(name)
	if err != nil {
		return err
	}

	parse := func(key string) []*x509.Certificate {
		certs, err := secrets.ParseCertificatesPEM(secrets.SecretData(secret, key))
		if err != nil {
			return nil
		}
		return certs
	}

	var certs []*x509.Certificate
	switch secrets.ClassifyCachedSecret(name, secret) {
	case secrets.CACertificateRole:
		certs = parse("ca.crt")
	case secrets.CARootKeyPairRole, secrets.CAKeyPairRole, secrets.UnusedCAKeyPairRole:
		if certs = parse("ca.crt"); certs == nil {
			certs = parse("tls.crt")
		}
	case secrets.TLSCertificateRole:
		certs = parse("tls.crt")
	}
	if certs == nil {
		return errors.Errorf("secret %q does not contain a certificate", name)
	}

	_, err = w.Write(secrets.EncodeCertificatesPEM(certs))

	return err
}
