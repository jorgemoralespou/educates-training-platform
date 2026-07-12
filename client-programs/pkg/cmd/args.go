package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// exactArgs returns a cobra Args validator that requires exactly n
// positional arguments, reporting a meaningful error through
// utils.CmdError (message describes what is missing, hint names the
// expected arguments and is appended after the command path) rather than
// cobra's terse "accepts N arg(s), received M".
func exactArgs(n int, message, hint string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != n {
			return utils.CmdError(cmd, message, hint)
		}
		return nil
	}
}

// maximumArgs returns a cobra Args validator that allows at most n
// positional arguments, reporting a meaningful error through
// utils.CmdError when more are supplied.
func maximumArgs(n int, message, hint string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) > n {
			return utils.CmdError(cmd, message, hint)
		}
		return nil
	}
}
