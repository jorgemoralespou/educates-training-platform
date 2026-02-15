package cmd

import (
	"os"
	"path/filepath"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type BundleExportOptions struct {
	Repository      string
	WorkshopFile    string
	WorkshopVersion string
	Workshops       []string
	AsFiles         string
	DataValuesFlags yttcmd.DataValuesFlags
}

const bundleExportExample = `
  # Export TrainingPortal and all workshop resource definitions from current bundle directory
  educates bundle export

  # Export TrainingPortal and selected workshop definitions
  educates bundle export --workshop lab-one --workshop lab-two

  # Export bundle resources as files into a directory
  educates bundle export --as-files ./export
`

func (o *BundleExportOptions) Run(args []string) error {
	var directory string
	if len(args) != 0 {
		directory = filepath.Clean(args[0])
	} else {
		directory = "."
	}

	var err error
	if directory, err = filepath.Abs(directory); err != nil {
		return errors.Wrap(err, "couldn't convert bundle directory to absolute path")
	}

	fileInfo, err := os.Stat(directory)
	if err != nil || !fileInfo.IsDir() {
		return errors.New("bundle directory does not exist or path is not a directory")
	}

	config := educates.ExportWorkshopBundleConfig{
		ExportWorkshopDefinitionConfig: educates.ExportWorkshopDefinitionConfig{
			Repository:      o.Repository,
			WorkshopFile:    o.WorkshopFile,
			WorkshopVersion: o.WorkshopVersion,
			DataValuesFlags: o.DataValuesFlags,
		},
		Workshops: o.Workshops,
	}

	manager := educates.NewWorkshopDefinitionManager()
	documents, err := manager.ExportBundle(directory, &config)
	if err != nil {
		return err
	}

	if o.AsFiles != "" {
		targetDirectory := o.AsFiles
		if !filepath.IsAbs(targetDirectory) {
			targetDirectory = filepath.Join(directory, targetDirectory)
		}
		return utils.WriteExportedDocuments(targetDirectory, documents)
	}

	return utils.PrintExportedDocuments(documents)
}

func (p *ProjectInfo) NewBundleExportCmd() *cobra.Command {
	var o BundleExportOptions

	var c = &cobra.Command{
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return utils.CmdError(cmd, "too many arguments", "[PATH]")
			}
			return nil
		},
		Use:     "export [PATH]",
		Short:   "Export bundle TrainingPortal and workshop resources",
		RunE:    func(_ *cobra.Command, args []string) error { return o.Run(args) },
		Example: bundleExportExample,
	}

	c.Flags().StringVar(
		&o.Repository,
		"image-repository",
		"localhost:5001",
		"the address of the image repository",
	)
	c.Flags().StringVar(
		&o.WorkshopFile,
		"workshop-file",
		constants.DefaultWorkshopDefinitionPath,
		"location of the workshop definition file relative to each workshop directory",
	)
	c.Flags().StringVar(
		&o.WorkshopVersion,
		"workshop-version",
		"latest",
		"version of the workshops being exported",
	)
	c.Flags().StringSliceVar(
		&o.Workshops,
		"workshop",
		nil,
		"export only these workshops by name (repeatable)",
	)
	c.Flags().StringVar(
		&o.AsFiles,
		"as-files",
		"",
		"write YAML resources as files in target directory instead of stdout",
	)

	c.Flags().StringArrayVar(
		&o.DataValuesFlags.EnvFromStrings,
		"data-values-env",
		nil,
		"Extract data values (as strings) from prefixed env vars (format: PREFIX for PREFIX_all__key1=str) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.EnvFromYAML,
		"data-values-env-yaml",
		nil,
		"Extract data values (parsed as YAML) from prefixed env vars (format: PREFIX for PREFIX_all__key1=true) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.KVsFromStrings,
		"data-value",
		nil,
		"Set specific data value to given value, as string (format: all.key1.subkey=123) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.KVsFromYAML,
		"data-value-yaml",
		nil,
		"Set specific data value to given value, parsed as YAML (format: all.key1.subkey=true) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.KVsFromFiles,
		"data-value-file",
		nil,
		"Set specific data value to contents of a file (format: [@lib1:]all.key1.subkey={file path, HTTP URL, or '-' (i.e. stdin)}) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.FromFiles,
		"data-values-file",
		nil,
		"Set multiple data values via plain YAML files (format: [@lib1:]{file path, HTTP URL, or '-' (i.e. stdin)}) (can be specified multiple times)",
	)

	return c
}
