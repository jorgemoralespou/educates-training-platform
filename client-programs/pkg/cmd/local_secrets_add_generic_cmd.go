package cmd

import (
	"github.com/spf13/cobra"
)

var localSecretsAddGenericExample = `
  # Create a secret from a literal value:
  educates local secrets add generic mysecret --from-literal key=value

  # Create a secret from a file:
  educates local secrets add generic mysecret --from-file ./path/to/file
`

type LocalSecretsAddGenericOptions struct {
	FileSources    []string
	LiteralSources []string
}

func (o *LocalSecretsAddGenericOptions) Run(name string) error {
	return nil
}

func (p *ProjectInfo) NewLocalSecretsAddGenericCmd() *cobra.Command {
	var o LocalSecretsAddGenericOptions

	var c = &cobra.Command{
		Args:    secretNameArgs,
		Use:     "generic NAME",
		Short:   "Create a secret from a local file, directory, or literal value",
		Example: localSecretsAddGenericExample,
		RunE:    func(_ *cobra.Command, args []string) error { return o.Run(args[0]) },
	}

	c.Flags().StringArrayVar(
		&o.FileSources,
		"from-file",
		[]string{},
		"Key files can be specified using their file path, in which case a default name will be given to them, or optionally with a name and file path, in which case the given name will be used. Specifying a directory will iterate each named file in the directory that is avalid secret key.",
	)
	c.Flags().StringArrayVar(
		&o.LiteralSources,
		"from-literal",
		[]string{},
		"Specify a key and literal value to insert in secret (i.e. mykey=somevalue)",
	)

	return c
}
