package cmd

import (
	"os"
	"regexp"

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

func (o *LocalSecretsImportOptions) Run() error {
	data, err := os.ReadFile(o.File)

	if err != nil {
		return errors.Wrapf(err, "unable to read secrets file %q", o.File)
	}

	regex := regexp.MustCompile("\n?---\n?")

	for i, yamlData := range regex.Split(string(data), -1) {
		decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDecoder()
		secretObj := &apiv1.Secret{}
		err = runtime.DecodeInto(decoder, []byte(yamlData), secretObj)

		if err != nil {
			return errors.Wrapf(err, "unable to decode secret #%d", i)
		}

		// Make sure that the namespace is cleared.

		secretObj.ObjectMeta.Namespace = ""

		if err := secrets.WriteCachedSecret(secretObj); err != nil {
			return err
		}
	}

	return nil
}

func (p *ProjectInfo) NewLocalSecretsImportCmd() *cobra.Command {
	var o LocalSecretsImportOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "import",
		Short:   "Import secrets to the cache",
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
