package utils

import (
	"fmt"

	"github.com/spf13/cobra"
)

// CmdError builds an error for a failed command-argument validation. It is
// meant to be returned from a cobra command's Args function so that an
// invalid invocation reports a clear message followed by the command path
// (with a usage hint) and a pointer to --help, rather than cobra's terse
// default ("accepts 1 arg(s), received 0").
//
// additionalMessage is the positional-argument hint appended after the
// command path, e.g. "NAME" so the user sees the expected argument.
func CmdError(cmd *cobra.Command, errorMessage string, additionalMessage string) error {
	return cmdError(cmd, errorMessage, additionalMessage, false)
}

// CmdErrorFullUsage is like CmdError but appends the command's full usage
// string instead of a single hint line. Prefer it for commands that take
// several positional arguments, where the full usage is clearer than a
// short hint.
func CmdErrorFullUsage(cmd *cobra.Command, errorMessage string, additionalMessage string) error {
	return cmdError(cmd, errorMessage, additionalMessage, true)
}

func cmdError(cmd *cobra.Command, errorMessage string, additionalMessage string, fullUsage bool) error {
	if fullUsage {
		return fmt.Errorf("%s\n\n%s", errorMessage, cmd.UsageString())
	}

	return fmt.Errorf("%s\n\n%s %s\nRun '%s --help' for details.", errorMessage, cmd.CommandPath(), additionalMessage, cmd.CommandPath())
}
