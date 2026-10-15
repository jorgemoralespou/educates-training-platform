package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/kubectl/pkg/scheme"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

var localSecretsImportExample = `
  # Import secrets from a YAML file into the local cache:
  educates local secrets import --file secrets.yaml
`

type LocalSecretsImportOptions struct {
	File string
}

// Run adds the Secrets of a YAML file to the local secrets cache,
// replacing cached Secrets of the same names. It checks every Secret
// before writing any, so a file it refuses leaves the cache unchanged.
//
// The cache holds one root CA, so the file may hold at most one CA root
// key pair, and only when the cache has no root CA or has that same
// root.
func (o *LocalSecretsImportOptions) Run() error {
	data, err := os.ReadFile(o.File)

	if err != nil {
		return errors.Wrapf(err, "unable to read secrets file %q", o.File)
	}

	regex := regexp.MustCompile("\n?---\n?")

	var imported []*apiv1.Secret

	for i, yamlData := range regex.Split(string(data), -1) {
		// The "---" before the first document, as export prints it,
		// leaves an empty document.
		if strings.TrimSpace(yamlData) == "" {
			continue
		}

		decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDecoder()
		secretObj := &apiv1.Secret{}
		err = runtime.DecodeInto(decoder, []byte(yamlData), secretObj)

		if err != nil {
			return errors.Wrapf(err, "unable to decode secret #%d", i)
		}

		if secretObj.ObjectMeta.Name == "" {
			return errors.Errorf("secret #%d has no name", i)
		}

		// Make sure that the namespace is cleared.

		secretObj.ObjectMeta.Namespace = ""

		imported = append(imported, secretObj)
	}

	if err := checkImportedRootCAs(imported); err != nil {
		return err
	}

	for _, secretObj := range imported {
		if err := secrets.WriteCachedSecret(secretObj); err != nil {
			return err
		}
	}

	return nil
}

// checkImportedRootCAs enforces the one-root rule of the local secrets
// cache on the Secrets an import adds: a Secret named with
// secrets.RootKeyPairSuffix is a CA root key pair, and the cache may end
// up with only one. Importing the root CA the cache already holds, such
// as from an export of the same cache, is allowed.
func checkImportedRootCAs(imported []*apiv1.Secret) error {
	importedRoots := map[string]*apiv1.Secret{}
	var names []string
	for _, s := range imported {
		if root, ok := strings.CutSuffix(s.Name, secrets.RootKeyPairSuffix); ok {
			if _, seen := importedRoots[root]; !seen {
				names = append(names, root)
			}
			importedRoots[root] = s
		}
	}

	switch len(names) {
	case 0:
		return nil
	case 1:
	default:
		return errors.Errorf(`the secrets file holds more than one root CA: %s.

The local secrets cache holds one root CA, from which deploys mint a CA
for each ingress domain. Import a file that holds only one of them.`, secrets.QuoteNames(names))
	}

	root := names[0]
	cachedRoots, err := secrets.CachedRootCAs()
	if err != nil {
		return err
	}
	if len(cachedRoots) == 0 {
		return nil
	}
	if len(cachedRoots) == 1 && cachedRoots[0] == root && sameRootCertificate(root, importedRoots[root]) {
		return nil
	}

	return rootCAAlreadyRegisteredError(cachedRoots, fmt.Sprintf("import the root CA %q", root))
}

// sameRootCertificate reports whether keyPair, an imported CA root key
// pair, holds the certificate of the cached root CA root.
func sameRootCertificate(root string, keyPair *apiv1.Secret) bool {
	cached, err := secrets.LoadCachedSecret(secrets.RootKeyPairName(root))
	if err != nil {
		return false
	}
	cachedCert, err := secrets.ParseCertificatePEM(secrets.SecretData(cached, "tls.crt"))
	if err != nil {
		return false
	}
	importedCert, err := secrets.ParseCertificatePEM(secrets.SecretData(keyPair, "tls.crt"))
	return err == nil && importedCert.Equal(cachedCert)
}

func (p *ProjectInfo) NewLocalSecretsImportCmd() *cobra.Command {
	var o LocalSecretsImportOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "import",
		Short: "Import secrets to the cache",
		Long: `Import the secrets in a YAML file into the local secrets cache,
replacing cached secrets of the same names.

The cache holds one root CA, so an import that would add a second CA root
key pair (a secret named NAME-root-key-pair) is refused, naming the root
CA already cached. Importing the root CA the cache already holds is
allowed. A refused import leaves the cache unchanged.`,
		Example: localSecretsImportExample,
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run() },
	}

	c.Flags().StringVarP(
		&o.File,
		"file",
		"f",
		"",
		"path to file of secrets to import",
	)

	c.MarkFlagRequired("file")

	return c
}
