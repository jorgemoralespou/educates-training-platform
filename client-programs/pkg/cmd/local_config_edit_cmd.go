package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

func (p *ProjectInfo) NewLocalConfigEditCmd() *cobra.Command {
	return &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "edit",
		Short: "Open <data-home>/config.yaml in $EDITOR; validate against the schema on save",
		Long: `Opens <data-home>/config.yaml in $EDITOR (or vi if unset) in a temp
copy. On editor exit, the temp copy is validated against the
EducatesLocalConfig schema; a passing validation atomically replaces
the canonical file. A failing validation leaves the canonical file
untouched and returns the schema error so the user can re-run and try
again.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLocalConfigEdit()
		},
	}
}

func runLocalConfigEdit() error {
	dataHome := utils.GetEducatesHomeDir()
	if err := os.MkdirAll(dataHome, 0o755); err != nil {
		return fmt.Errorf("create data home %q: %w", dataHome, err)
	}
	canonical := filepath.Join(dataHome, "config.yaml")
	tmp := fmt.Sprintf("%s.%d", canonical, os.Getpid())
	defer os.Remove(tmp)

	// Seed the temp file with current contents (or the minimal init
	// stub when the canonical doesn't exist yet — so 'edit' on a
	// pristine data home is a working flow).
	seed, err := os.ReadFile(canonical)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read %s: %w", canonical, err)
		}
		seed = []byte(defaultLocalConfigYAML)
	}
	if err := os.WriteFile(tmp, seed, 0o644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	editor := "vi"
	if v := os.Getenv("EDITOR"); v != "" {
		editor = v
	}
	editorPath, err := exec.LookPath(editor)
	if err != nil {
		return fmt.Errorf("locate editor %q: %w", editor, err)
	}

	cmd := exec.Command(editorPath, tmp)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor: %w", err)
	}

	if _, err := config.LoadLocal(tmp); err != nil {
		return fmt.Errorf("validation failed; canonical file left unchanged: %w", err)
	}

	if err := os.Rename(tmp, canonical); err != nil {
		return fmt.Errorf("replace %s: %w", canonical, err)
	}
	return nil
}
