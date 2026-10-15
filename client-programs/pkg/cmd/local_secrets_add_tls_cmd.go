package cmd

import (
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

var localSecretsAddTlsExample = `
  # Cache a mkcert wildcard certificate, and mkcert's root CA certificate
  # so Educates trusts it:
  mkcert -cert-file workshop.test.pem -key-file workshop.test-key.pem "*.workshop.test"
  educates local secrets add tls workshop.test-tls --cert workshop.test.pem --key workshop.test-key.pem
  educates local secrets add ca mkcert-ca --cert "$(mkcert -CAROOT)/rootCA.pem"
`

type LocalSecretsAddTlsOptions struct {
	CertFile string
	KeyFile  string
}

// Run caches under name the wildcard TLS certificate in CertFile, with
// any intermediate certificates following it, and its private key in
// KeyFile. The certificate must not be a CA, the key must match it, and
// it must have at least one *.<domain> DNS SAN. The cached Secret
// records no domain: the domains it serves are its wildcard SANs.
func (o *LocalSecretsAddTlsOptions) Run(name string) error {
	if o.CertFile == "" || o.KeyFile == "" {
		return errors.New("add tls needs --cert and --key: pass the wildcard certificate together with its private key")
	}
	if suffix := reservedNameSuffix(name); suffix != "" {
		return errors.Errorf("invalid TLS certificate name %q: names ending in %q are reserved for the CA key pairs the local secrets cache manages", name, suffix)
	}
	roots, err := secrets.CachedRootCAs()
	if err != nil {
		return err
	}
	for _, root := range roots {
		if root == name {
			return errors.Errorf("%q is the root CA registered in the local secrets cache; choose another name for this TLS certificate", name)
		}
	}

	certData, err := os.ReadFile(o.CertFile)
	if err != nil {
		return errors.Wrapf(err, "failed to read certificate file %s", o.CertFile)
	}
	chain, err := secrets.ParseCertificatesPEM(certData)
	if err != nil {
		return errors.Wrapf(err, "invalid certificate file %s", o.CertFile)
	}
	cert := chain[0]
	if secrets.IsCACertificate(cert) {
		return errors.Errorf("the certificate in %s (subject %q) is a CA certificate, not a wildcard certificate; register a CA with 'educates local secrets add ca'", o.CertFile, cert.Subject.String())
	}
	keyData, err := os.ReadFile(o.KeyFile)
	if err != nil {
		return errors.Wrapf(err, "failed to read key file %s", o.KeyFile)
	}
	key, err := secrets.ParsePrivateKeyPEM(keyData)
	if err != nil {
		return errors.Wrapf(err, "invalid key file %s", o.KeyFile)
	}
	if !secrets.KeyMatchesCertificate(cert, key) {
		return errors.Errorf("the private key in %s does not match the certificate in %s", o.KeyFile, o.CertFile)
	}
	if len(secrets.WildcardDomains(cert)) == 0 {
		return errors.Errorf("the certificate in %s has no wildcard DNS name (*.<domain>) among its SANs %v; Educates serves one wildcard certificate for every workshop host of an ingress domain", o.CertFile, cert.DNSNames)
	}

	var certPEM []byte
	for _, c := range chain {
		certPEM = append(certPEM, secrets.EncodeCertificatePEM(c)...)
	}
	keyPEM, err := secrets.EncodePrivateKeyPEM(key)
	if err != nil {
		return err
	}

	return secrets.WriteCachedSecret(secrets.NewTLSSecret(name, certPEM, keyPEM, ""))
}

// reservedNameSuffix returns the suffix of name that the local secrets
// cache reserves for the CA key pairs it manages, or "" when name ends in
// none of them.
func reservedNameSuffix(name string) string {
	for _, suffix := range []string{secrets.RootKeyPairSuffix, secrets.CAKeyPairSuffix} {
		if strings.HasSuffix(name, suffix) {
			return suffix
		}
	}
	return ""
}

func (p *ProjectInfo) NewLocalSecretsAddTlsCmd() *cobra.Command {
	var o LocalSecretsAddTlsOptions

	var c = &cobra.Command{
		Args:  secretNameArgs,
		Use:   "tls NAME",
		Short: "Cache a wildcard TLS certificate",
		Long: `Cache a wildcard TLS certificate in the local secrets cache as NAME.

--cert is the certificate, optionally followed by its intermediate
certificates, and --key its private key. The certificate must not be a
CA, the key must match it, and it must have at least one *.<domain> DNS
name. It serves every domain it has a wildcard DNS name for.

When exactly one cached wildcard certificate covers the ingress domain, a
deploy serves it with the StaticCertificate certificates provider and
installs no cert-manager. Register the CA that issued it with
'educates local secrets add ca NAME --cert', so Educates trusts it.`,
		Example: localSecretsAddTlsExample,
		RunE:    func(_ *cobra.Command, args []string) error { return o.Run(args[0]) },
	}

	c.Flags().StringVar(
		&o.CertFile,
		"cert",
		"",
		"path to the PEM-encoded wildcard certificate, optionally followed by its intermediate certificates",
	)
	c.Flags().StringVar(
		&o.KeyFile,
		"key",
		"",
		"path to the PEM-encoded private key of the certificate",
	)

	c.MarkFlagRequired("cert")
	c.MarkFlagRequired("key")

	return c
}
