package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

const defaultLocalConfigYAML = `apiVersion: ` + v1alpha1.APIVersion + `
kind: ` + v1alpha1.KindEducatesLocalConfig + `
`

type LocalConfigInitOptions struct {
	Force bool
}

func (p *ProjectInfo) NewLocalConfigInitCmd() *cobra.Command {
	var o LocalConfigInitOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "init",
		Short: "Write a default EducatesLocalConfig to <data-home>/config.yaml",
		Long: `Creates <data-home>/config.yaml with the minimum EducatesLocalConfig
(apiVersion + kind only). All other fields take their schema defaults at
deploy time. Errors if the file already exists unless --force.

<data-home> is $EDUCATES_CLI_DATA_HOME if set, otherwise
$XDG_DATA_HOME/educates.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return o.Run(cmd.OutOrStdout())
		},
	}
	c.Flags().BoolVar(&o.Force, "force", false, "overwrite existing config.yaml")
	return c
}

func (o *LocalConfigInitOptions) Run(w interface{ Write([]byte) (int, error) }) error {
	dataHome := utils.GetEducatesHomeDir()
	if err := os.MkdirAll(dataHome, 0o755); err != nil {
		return fmt.Errorf("create data home %q: %w", dataHome, err)
	}
	path := filepath.Join(dataHome, "config.yaml")
	if _, err := os.Stat(path); err == nil && !o.Force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", path)
	}
	if err := os.WriteFile(path, []byte(defaultLocalConfigYAML), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintf(w, "wrote %s\n", path)
	return nil
}
