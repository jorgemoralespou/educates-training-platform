package cmd

import (
	"github.com/spf13/cobra"
)

var projectVersionExample = `
  # Print the Educates CLI version:
  educates version

  # Print the version with build commit details:
  educates version --full
`

type ProjectVersionOptions struct {
	full bool
}

/*
Create Cobra command object for displaying Educates version.
*/
func (p *ProjectInfo) NewProjectVersionCmd() *cobra.Command {
	var o ProjectVersionOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "version",
		Short:   "Display the version of Educates being used",
		Example: projectVersionExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fullVersion := p.Version
			if o.full {
				if p.GitCommit != "" {
					commit := "commit: " + p.GitCommit
					if p.BuildDate != "" {
						commit += " (" + p.BuildDate + ")"
					}
					fullVersion += " [" + commit + "]"
				}
			}
			cmd.Println(fullVersion)
			return nil
		},
	}

	c.Flags().BoolVar(
		&o.full,
		"full",
		false,
		"full version details",
	)

	return c
}
