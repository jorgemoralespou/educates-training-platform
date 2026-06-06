package utils

import (
	"os"
	"path"

	"github.com/adrg/xdg"
)

// EducatesCLIDataHomeEnv overrides the on-disk home for all Educates CLI
// state. When set and non-empty, it takes precedence over the default
// xdg.DataHome/educates location. Useful for CI, multi-instance laptop
// workflows, and tests.
const EducatesCLIDataHomeEnv = "EDUCATES_CLI_DATA_HOME"

// GetEducatesHomeDir returns the on-disk home for all Educates CLI state
// (config.yaml, secrets/, kind/, resolver/, workshops/).
//
// Resolution order:
//  1. $EDUCATES_CLI_DATA_HOME if set and non-empty.
//  2. $XDG_DATA_HOME/educates/ (default).
func GetEducatesHomeDir() string {
	if v := os.Getenv(EducatesCLIDataHomeEnv); v != "" {
		return v
	}
	return path.Join(xdg.DataHome, "educates")
}
