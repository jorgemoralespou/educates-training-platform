package cmd

import (
	"fmt"
	"os"
	"path"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	apiv1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

func (p *ProjectInfo) NewLocalSecretsExportCmd() *cobra.Command {
	var pem bool

	var c = &cobra.Command{
		Args:  cobra.ArbitraryArgs,
		Use:   "export [NAME]",
		Short: "Export secrets in the cache",
		RunE: func(_ *cobra.Command, args []string) error {
			secretsCacheDir := path.Join(utils.GetEducatesHomeDir(), "secrets")

			err := os.MkdirAll(secretsCacheDir, os.ModePerm)

			if err != nil {
				return errors.Wrapf(err, "unable to create secrets cache directory")
			}

			if pem {
				if len(args) != 1 {
					return errors.New("--pem requires exactly one secret name")
				}

				return printSecretCertificatePEM(secretsCacheDir, args[0])
			}

			err = utils.PrintYamlFilesInDir(secretsCacheDir, args)
			if err != nil {
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
// secret to stdout as PEM, so it can be redirected to a file and
// imported into an operating system trust store. The private key is
// never printed.
func printSecretCertificatePEM(secretsCacheDir string, name string) error {
	secretFilePath := path.Join(secretsCacheDir, name+".yaml")

	secretData, err := os.ReadFile(secretFilePath)

	if err != nil {
		if os.IsNotExist(err) {
			return errors.Errorf("no secret named %q in the local secrets cache", name)
		}

		return errors.Wrapf(err, "unable to read secret file %s", secretFilePath)
	}

	var secret apiv1.Secret

	if err := yaml.Unmarshal(secretData, &secret); err != nil {
		return errors.Wrapf(err, "unable to parse secret file %s", secretFilePath)
	}

	certificate, exists := secret.Data["tls.crt"]

	if !exists || len(certificate) == 0 {
		return errors.Errorf("secret %q does not contain a certificate", name)
	}

	fmt.Print(string(certificate))

	return nil
}
