package cmd

import (
	"crypto/x509"
	"fmt"
	"io"
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	apiv1 "k8s.io/api/core/v1"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

var localSecretsAddCaExample = `
  # Generate a root CA and cache it locally:
  educates local secrets add ca educates-ca

  # Register an existing root CA, such as mkcert's, with its private key:
  educates local secrets add ca mkcert-ca \
    --cert "$(mkcert -CAROOT)/rootCA.pem" --key "$(mkcert -CAROOT)/rootCA-key.pem"

  # Register a CA certificate only, for trust:
  educates local secrets add ca corporate-ca --cert corporate-ca.pem
`

type LocalSecretsAddCaOptions struct {
	CertFile string
	KeyFile  string
}

// Run caches CA material under name. With no files it generates a root
// CA; with a certificate and its key it registers them as the root CA;
// with a certificate alone it caches a CA certificate for trust only.
//
// A root CA is cached as its CA trust Secret, name, holding the root
// certificate as ca.crt, and its CA root key pair, which holds the key
// and never leaves this machine. Deploys mint a CA for each ingress
// domain from the root. The cache holds at most one root.
func (o *LocalSecretsAddCaOptions) Run(w io.Writer, name string) error {
	if suffix := reservedNameSuffix(name); suffix != "" {
		return errors.Errorf("invalid CA name %q: names ending in %q are reserved for the CA key pairs the local secrets cache manages", name, suffix)
	}
	if o.KeyFile != "" && o.CertFile == "" {
		return errors.New("--key needs --cert: pass the root CA certificate together with its private key")
	}

	roots, err := secrets.CachedRootCAs()
	if err != nil {
		return err
	}

	if o.CertFile != "" && o.KeyFile == "" {
		// A CA certificate cached for trust only. It must not replace the
		// certificate of a root, which deploys reference as its trust.
		for _, root := range roots {
			if root == name {
				return errors.Errorf("%q is the root CA registered in the local secrets cache; choose another name for this CA certificate", name)
			}
		}
		cert, err := readCACertificate(o.CertFile)
		if err != nil {
			return err
		}
		return secrets.WriteCachedSecret(secrets.NewCATrustSecret(name, secrets.EncodeCertificatePEM(cert)))
	}

	if len(roots) > 0 {
		return errors.Errorf(`a root CA is already registered in the local secrets cache: %q.

The cache holds one root CA, from which deploys mint a CA for each ingress
domain. To register another root, remove the existing one first:

  educates local secrets remove %s`, roots[0], roots[0])
	}

	var certPEM, keyPEM []byte
	generated := o.CertFile == ""
	if generated {
		certPEM, keyPEM, err = secrets.GenerateSelfSignedCA("educates-dev-ca (" + name + ")")
		if err != nil {
			return errors.Wrap(err, "failed to generate root CA")
		}
	} else {
		cert, err := readCACertificate(o.CertFile)
		if err != nil {
			return err
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
			return errors.Errorf("the private key in %s does not match the CA certificate in %s", o.KeyFile, o.CertFile)
		}
		certPEM = secrets.EncodeCertificatePEM(cert)
		if keyPEM, err = secrets.EncodePrivateKeyPEM(key); err != nil {
			return err
		}
	}

	for _, secret := range []*apiv1.Secret{
		secrets.NewCATrustSecret(name, certPEM),
		secrets.NewCARootKeyPairSecret(name, certPEM, keyPEM),
	} {
		if err := secrets.WriteCachedSecret(secret); err != nil {
			return err
		}
	}

	if generated {
		fmt.Fprintf(w, `Generated root CA %q and cached it locally. Its private key stays on
this machine: each deploy mints a CA limited to its ingress domain from
this root, and only that CA reaches the cluster.

Browsers will not trust workshop URLs until this root CA is imported into
your operating system trust store. You import it once, for every local
ingress domain. Export the root CA certificate with:

    educates local secrets export %s --pem > %s.pem

then import the PEM file into your trust store. See the quick start
guide in the documentation for per-platform instructions.
`, name, name, name)
	}

	return nil
}

// readCACertificate reads the first certificate of a PEM file and checks
// it is a CA.
func readCACertificate(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read certificate file %s", path)
	}
	cert, err := secrets.ParseCertificatePEM(data)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid certificate file %s", path)
	}
	if !secrets.IsCACertificate(cert) {
		return nil, errors.Errorf("the certificate in %s (subject %q) is not a CA certificate", path, cert.Subject.String())
	}
	return cert, nil
}

func (p *ProjectInfo) NewLocalSecretsAddCaCmd() *cobra.Command {
	var o LocalSecretsAddCaOptions

	var c = &cobra.Command{
		Args:  secretNameArgs,
		Use:   "ca NAME",
		Short: "Create or register a CA",
		Long: `Create or register a CA in the local secrets cache.

With no flags, generates a root CA. With --cert and --key, registers an
existing root CA, such as mkcert's. Either way the root certificate is
cached as NAME and its private key as NAME-root-key-pair, which is never
copied to a cluster. Deploys mint a CA for each ingress domain from the
root. The cache holds one root CA.

With --cert alone, caches a CA certificate as NAME for trust only.`,
		Example: localSecretsAddCaExample,
		RunE:    func(cmd *cobra.Command, args []string) error { return o.Run(cmd.OutOrStdout(), args[0]) },
	}

	c.Flags().StringVar(
		&o.CertFile,
		"cert",
		"",
		"path to a PEM-encoded CA certificate (omit to generate a root CA)",
	)
	c.Flags().StringVar(
		&o.KeyFile,
		"key",
		"",
		"path to the PEM-encoded private key of the --cert CA, to register it as the root CA",
	)

	return c
}
