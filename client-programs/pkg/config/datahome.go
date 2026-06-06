package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// MissingLocalConfigError diagnoses why <data-home>/config.yaml is missing
// and returns a user-actionable error. Three cases:
//
//  1. v3 `values.yaml` exists alongside the missing config.yaml — the user
//     is on a pre-v4 data home and needs the migration shim (planned step 10
//     of the phase 5 implementation; not yet landed).
//  2. The data home directory itself doesn't exist — first-time user.
//  3. The directory exists but config.yaml is missing — initialised data
//     home (e.g. secrets/ present from past CLI runs) but no v4 config yet.
//
// Returns nil if the data home looks healthy and config.yaml is present;
// callers should not invoke this in that case (the loader handles it).
func MissingLocalConfigError(dataHome string) error {
	configPath := filepath.Join(dataHome, "config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		// Caller misuse — config.yaml does exist. Surface a generic
		// message so the bug is visible.
		return fmt.Errorf("internal: MissingLocalConfigError called but %s exists", configPath)
	}

	v3Values := filepath.Join(dataHome, "values.yaml")
	if _, err := os.Stat(v3Values); err == nil {
		return fmt.Errorf(`no v4 config found at %s, but a v3-style values.yaml is present at %s.

A first-run migration that translates v3 values.yaml into v4 config.yaml is
planned (phase 5 step 10) but not yet implemented. Until then, you can:

  1. Create a minimal v4 config by hand:
       printf 'apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n' \
         > %q
     Then edit %q and copy across any non-default
     settings from values.yaml (ingress.domain, resolver.*, etc.).

  2. Or point at an explicit v4 config:
       educates admin platform render --config <your-v4-config.yaml>

The existing values.yaml is left untouched; the upcoming migration shim
will translate it in place (and rename the original to values.yaml.v3-backup)
when it lands.`, configPath, v3Values, configPath, configPath)
	}

	if _, err := os.Stat(dataHome); os.IsNotExist(err) {
		return fmt.Errorf(`no Educates data home found at %s.

First-time setup: create a minimal config and proceed. Until the upcoming
'educates local config init' lands (phase 5 step 7), do it by hand:

  mkdir -p %q
  printf 'apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n' \
    > %q

Then re-run your command.`, dataHome, dataHome, filepath.Join(dataHome, "config.yaml"))
	}

	return fmt.Errorf(`no v4 config found at %s.

The data home directory exists but config.yaml is missing. Until the upcoming
'educates local config init' lands (phase 5 step 7), create one by hand:

  printf 'apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n' \
    > %s

Then re-run your command.`, configPath, configPath)
}
