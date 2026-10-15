package cmd

import (
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

func (p *ProjectInfo) NewLocalSecretsAddCmdGroup() *cobra.Command {
	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "add",
		Short: "Add secret to the cache",
	}

	// Use a command group as it allows us to dictate the order in which they
	// are displayed in the help message, as otherwise they are displayed in
	// sort order.

	commandGroups := templates.CommandGroups{
		{
			Message: "Available Commands:",
			Commands: []*cobra.Command{
				p.NewLocalSecretsAddCaCmd(),
				p.NewLocalSecretsAddDockerRegistryCmd(),
				// NewLocalSecretsAddGenericCmd(),
				p.NewLocalSecretsAddTlsCmd(),
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	return c
}

// secretNameArgs builds a cobra Args validator that requires exactly one
// NAME argument and validates its shape, returning a meaningful error
// (with the command path and a --help pointer) before any work runs. It is
// shared by every 'local secrets add' subcommand and by 'local secrets
// view' and 'local secrets remove'.
func secretNameArgs(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return utils.CmdError(cmd, "name is required", "NAME")
	}
	if err := secrets.ValidateSecretName(args[0]); err != nil {
		return utils.CmdError(cmd, err.Error(), "NAME")
	}
	return nil
}

// readKeyPairFiles reads a certificate, optionally followed by its chain,
// from certFile and its private key from keyFile, and checks the key
// matches the certificate. Its errors name the file at fault.
func readKeyPairFiles(certFile, keyFile string) (*secrets.KeyPair, error) {
	certData, err := os.ReadFile(certFile)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read certificate file %s", certFile)
	}
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read key file %s", keyFile)
	}
	kp, err := secrets.ParseKeyPair(certData, keyData)
	var kpErr *secrets.KeyPairError
	if errors.As(err, &kpErr) {
		switch kpErr.Problem {
		case secrets.KeyPairCertificateUnparseable:
			return nil, errors.Wrapf(kpErr.Err, "invalid certificate file %s", certFile)
		case secrets.KeyPairKeyUnparseable:
			return nil, errors.Wrapf(kpErr.Err, "invalid key file %s", keyFile)
		default:
			return nil, errors.Errorf("the private key in %s does not match the certificate in %s", keyFile, certFile)
		}
	}
	return kp, err
}

// rootCAAlreadyRegisteredError refuses a second root CA, naming the
// cached roots and how to remove one before doing what action says.
func rootCAAlreadyRegisteredError(roots []string, action string) error {
	return errors.Errorf(`a root CA is already registered in the local secrets cache: %s.

The cache holds one root CA, from which deploys mint a CA for each ingress
domain. To %s, remove the existing one first:

  educates local secrets remove %s`, secrets.QuoteNames(roots), action, roots[0])
}

// reservedNameError refuses name for a Secret of kind what when the name
// alone classifies a cached Secret as CA material the local secrets cache
// manages, and returns nil for any other name.
func reservedNameError(name, what string) error {
	if role, ok := secrets.ClassifyCachedSecretName(name); ok {
		return errors.Errorf("invalid %s name %q: it is reserved for a %s, which the local secrets cache manages", what, name, role)
	}
	return nil
}

// rootCANameError refuses to cache a Secret of kind what under name, the
// name of the root CA, whose CA trust Secret deploys reference.
func rootCANameError(name, what string) error {
	return errors.Errorf("%q is the root CA registered in the local secrets cache; choose another name for this %s", name, what)
}
