package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

func (p *ProjectInfo) NewLocalConfigViewCmd() *cobra.Command {
	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "view",
		Short: "Print <data-home>/config.yaml, validating it against the EducatesLocalConfig schema",
		Long: `Reads <data-home>/config.yaml, validates it against the
EducatesLocalConfig schema, and prints the raw file contents.

For programmatic field reads use 'educates local config get [PATH]' —
view's job is to surface the file as the user wrote it (including any
comments) plus assert it would load cleanly at deploy time.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLocalConfigView(cmd.OutOrStdout())
		},
	}
	return c
}

func runLocalConfigView(w interface{ Write([]byte) (int, error) }) error {
	cfgPath := filepath.Join(utils.GetEducatesHomeDir(), "config.yaml")
	if _, statErr := os.Stat(cfgPath); os.IsNotExist(statErr) {
		return config.MissingLocalConfigError(utils.GetEducatesHomeDir())
	}
	// Validate (Load runs the JSON schema check); we discard the typed
	// value because view's contract is to surface the raw file.
	if _, err := config.LoadLocal(cfgPath); err != nil {
		return fmt.Errorf("%s: %w", cfgPath, err)
	}
	body, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}
