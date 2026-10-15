package cmd

import (
	"crypto/x509"
	"io"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	apiv1 "k8s.io/api/core/v1"

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
// For a CA it prints the root certificate: the ca.crt of a CA key pair,
// and the certificate of a CA trust Secret or CA root key pair. For a TLS
// certificate it prints tls.crt, the certificate and its chain.
func printSecretCertificatePEM(w io.Writer, name string) error {
	secret, err := secrets.LoadCachedSecret(name)
	if err != nil {
		return err
	}

	tlsCerts, tlsErr := secrets.ParseCertificatesPEM(secretValue(secret, "tls.crt"))
	caCerts, caErr := secrets.ParseCertificatesPEM(secretValue(secret, "ca.crt"))

	var certs []*x509.Certificate
	switch {
	case caErr == nil && (tlsErr != nil || secrets.IsCACertificate(tlsCerts[0])):
		// A CA trust Secret, or CA material that records its root.
		certs = caCerts
	case tlsErr == nil:
		certs = tlsCerts
	default:
		return errors.Errorf("secret %q does not contain a certificate", name)
	}

	var out []byte
	for _, cert := range certs {
		out = append(out, secrets.EncodeCertificatePEM(cert)...)
	}
	_, err = w.Write(out)

	return err
}

// secretValue returns the value of key in s, from data or stringData.
func secretValue(s *apiv1.Secret, key string) []byte {
	if v, ok := s.Data[key]; ok {
		return v
	}
	if v, ok := s.StringData[key]; ok {
		return []byte(v)
	}
	return nil
}
