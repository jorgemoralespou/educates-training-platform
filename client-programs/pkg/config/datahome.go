package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnsureLocalConfigFile is the single entry point for commands that
// read <data-home>/config.yaml. It composes the v3-to-v4 migration
// shim with the user-actionable missing-file diagnostic:
//
//   - config.yaml exists → return nil (proceed to Load).
//   - config.yaml missing → attempt v3 → v4 migration. If the v3
//     migration writes a fresh config.yaml, return nil. If migration
//     refuses (provider isn't laptop-kind), surface that error.
//   - config.yaml still missing after migration attempt → return
//     MissingLocalConfigError (first-time user / partial init).
func EnsureLocalConfigFile(dataHome string) error {
	configPath := filepath.Join(dataHome, "config.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return nil
	}
	if err := MaybeMigrateV3(dataHome); err != nil {
		return err
	}
	if _, err := os.Stat(configPath); err == nil {
		return nil
	}
	return MissingLocalConfigError(dataHome)
}

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
		// EnsureLocalConfigFile would normally have triggered
		// MaybeMigrateV3 before reaching this branch; landing here
		// means migration refused (non-laptop provider) and the user
		// retried without reading that error. Re-state the path
		// forward briefly.
		return fmt.Errorf(`no v4 config found at %s, but a v3-style values.yaml is present at %s.

The migration shim only translates laptop-kind installs
(clusterInfrastructure.provider empty or "kind"). Other providers
need a fresh v4 config declared explicitly:

  educates admin platform render --config <your-v4-config.yaml>

The available v4 kinds live under cli.educates.dev/v1alpha1
(EducatesLocalConfig, EducatesConfig escape hatch, and the
scenario kinds GKE/EKS/Inline landing in phase 5 step 11).`,
			configPath, v3Values)
	}

	if _, err := os.Stat(dataHome); os.IsNotExist(err) {
		return fmt.Errorf(`no Educates data home found at %s.

First-time setup: write a minimal config and re-run.

  educates local config init

Or, with --config <file>, point at any v4 config file:

  educates admin platform render --config <your-v4-config.yaml>`, dataHome)
	}

	return fmt.Errorf(`no v4 config found at %s.

The data home directory exists but config.yaml is missing. Write a
minimal config and re-run:

  educates local config init

Or point at an explicit v4 config:

  educates admin platform render --config <your-v4-config.yaml>`, configPath)
}
