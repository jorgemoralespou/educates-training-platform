package cmd

import (
	"fmt"
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

var localSecretsAddCaExample = `
  # Generate a fresh self-signed CA and cache it locally:
  educates local secrets add ca mydomain-ca --domain mydomain.test

  # Import an existing CA certificate and private key:
  educates local secrets add ca mydomain-ca --cert ca.crt --key ca.key --domain mydomain.test
`

type LocalSecretsAddCaOptions struct {
	CertFile      string
	KeyFile       string
	IngressDomain string
}

func (o *LocalSecretsAddCaOptions) Run(name string) error {
	var err error

	// v4 contract: a CA in the local cache is a *signing* CA. The
	// CustomCA flow in EducatesLocalConfig hands it to cert-manager,
	// which signs workshop certs from it — both tls.crt AND tls.key
	// are required. Three input paths:
	//
	//   1. Both --cert and --key provided: load from disk.
	//   2. Neither provided: generate a fresh RSA-2048 self-signed
	//      CA in-process. Convenient for laptop use; the only
	//      side-effect on disk is the cached Secret YAML below.
	//   3. Only one provided: error (incomplete).
	//
	// Non-signing CA refs (cert only, for trust distribution) are no
	// longer supported here. Use EducatesConfig if you need that.
	var certPEM, keyPEM []byte
	var generated bool
	switch {
	case o.CertFile != "" && o.KeyFile != "":
		certPEM, err = os.ReadFile(o.CertFile)
		if err != nil {
			return errors.Wrapf(err, "failed to read certificate file %s", o.CertFile)
		}
		keyPEM, err = os.ReadFile(o.KeyFile)
		if err != nil {
			return errors.Wrapf(err, "failed to read key file %s", o.KeyFile)
		}
	case o.CertFile == "" && o.KeyFile == "":
		commonName := "educates-dev-ca"
		if o.IngressDomain != "" {
			commonName = "educates-dev-ca (" + o.IngressDomain + ")"
		}
		certPEM, keyPEM, err = secrets.GenerateSelfSignedCA(commonName)
		if err != nil {
			return errors.Wrap(err, "failed to generate self-signed CA")
		}
		generated = true
	default:
		return errors.New("--cert and --key must be provided together (or neither, to auto-generate a self-signed CA)")
	}

	secret := secrets.NewCASecret(name, certPEM, keyPEM, o.IngressDomain)

	if err := secrets.WriteCachedSecret(secret); err != nil {
		return err
	}

	if generated {
		fmt.Printf(`Generated a self-signed CA %q and cached it locally.

Browsers will not trust workshop URLs until this CA is imported into
your operating system trust store. Export the CA certificate with:

    educates local secrets export %s --pem > %s.pem

then import the PEM file into your trust store. See the quick start
guide in the documentation for per-platform instructions.
`, name, name, name)
	}

	return nil
}

func (p *ProjectInfo) NewLocalSecretsAddCaCmd() *cobra.Command {
	var o LocalSecretsAddCaOptions

	var c = &cobra.Command{
		Args:    secretNameArgs,
		Use:     "ca NAME",
		Short:   "Create a CA secret",
		Example: localSecretsAddCaExample,
		RunE:    func(_ *cobra.Command, args []string) error { return o.Run(args[0]) },
	}

	c.Flags().StringVar(
		&o.CertFile,
		"cert",
		"",
		"path to PEM-encoded CA certificate (omit both --cert and --key to auto-generate a self-signed CA)",
	)
	c.Flags().StringVar(
		&o.KeyFile,
		"key",
		"",
		"path to PEM-encoded CA private key (omit both --cert and --key to auto-generate)",
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
