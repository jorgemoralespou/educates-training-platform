package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/educates/educates-training-platform/client-programs/pkg/educates"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

const bundleNewExample = `
  # Create an empty workshop bundle project
  educates bundle new my-bundle

  # Create a workshop bundle project with two workshops
  educates bundle new my-bundle --workshop lab-one --workshop lab-two

  # Create a workshop bundle project in /tmp/workshops
  educates bundle new my-bundle -d /tmp/workshops

  # Create a workshop bundle project and overwrite existing files
  educates bundle new my-bundle -d . -y

  # Create a workshop bundle project with workshop template options
  educates bundle new my-bundle --workshop lab-one --template hugo --with-kubernetes-access

  # Create a workshop bundle project with project publish GitHub workflow
  educates bundle new my-bundle --with-github-action
`

type BundleNewOptions struct {
	Name                  string
	Template              string
	Title                 string
	Description           string
	Image                 string
	TargetDirectory       string
	Overwrite             bool
	WorkshopNames         []string
	WithGitHubAction      bool
	WithKubernetesAccess  bool
	WithVirtualCluster    bool
	WithDockerDaemon      bool
	WithImageRegistry     bool
	WithKubernetesConsole bool
	WithEditor            bool
	WithTerminal          bool
}

func (p *ProjectInfo) NewBundleNewCmd() *cobra.Command {
	var o BundleNewOptions

	var c = &cobra.Command{
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return utils.CmdError(cmd, "path is required", "PATH")
			}
			if len(args) > 1 {
				return utils.CmdError(cmd, "too many arguments", "PATH")
			}
			return nil
		},
		Use:   "new PATH",
		Short: "Create a workshop bundle project",
		RunE: func(_ *cobra.Command, args []string) error {
			name := o.Name
			if name == "" {
				name = filepath.Base(filepath.Clean(args[0]))
			}

			if match, _ := regexp.MatchString("^[a-z0-9-]+$", name); !match {
				return errors.Errorf("invalid bundle name %q", name)
			}

			for _, workshopName := range o.WorkshopNames {
				if match, _ := regexp.MatchString("^[a-z0-9-]+$", workshopName); !match {
					return errors.Errorf("invalid workshop name %q", workshopName)
				}
			}

			bundleDirectory := filepath.Clean(args[0])
			if o.TargetDirectory != "" {
				bundleDirectory = filepath.Join(o.TargetDirectory, args[0])
			}

			var err error
			if bundleDirectory, err = filepath.Abs(bundleDirectory); err != nil {
				return errors.Wrapf(err, "could not convert path name %q to absolute path", bundleDirectory)
			}

			if _, err = os.Stat(bundleDirectory); err == nil {
				ok := o.Overwrite
				if !o.Overwrite {
					ok = utils.YesNoPrompt([]string{
						fmt.Sprintf("WARNING: The directory %q already exists.", bundleDirectory),
						"All files will be created in it, overwriting existing files.",
						"Do you still want to use this directory?",
					}, true)
				}
				if !ok {
					return nil
				}
			}

			manager := educates.NewWorkshopDefinitionManager()
			err = manager.NewBundle(bundleDirectory, &educates.NewWorkshopBundleConfig{
				Name:                  name,
				Template:              o.Template,
				Title:                 o.Title,
				Description:           o.Description,
				Image:                 o.Image,
				Overwrite:             o.Overwrite,
				WorkshopNames:         o.WorkshopNames,
				WithGitHubAction:      o.WithGitHubAction,
				WithKubernetesAccess:  o.WithKubernetesAccess,
				WithVirtualCluster:    o.WithVirtualCluster,
				WithDockerDaemon:      o.WithDockerDaemon,
				WithImageRegistry:     o.WithImageRegistry,
				WithKubernetesConsole: o.WithKubernetesConsole,
				WithEditor:            o.WithEditor,
				WithTerminal:          o.WithTerminal,
			})
			if err != nil {
				return err
			}

			fmt.Printf("Workshop bundle %q created successfully.\n", name)
			return nil
		},
		Example: bundleNewExample,
	}

	c.Flags().StringVar(
		&o.Name,
		"name",
		"",
		"override name of the workshop bundle (default: directory name)",
	)
	c.Flags().StringVarP(
		&o.Template,
		"template",
		"t",
		"hugo",
		"name of the workshop template to use (hugo, classic)",
	)
	c.Flags().StringVarP(
		&o.TargetDirectory,
		"directory",
		"d",
		"",
		"directory where the workshop bundle will be created",
	)
	c.Flags().BoolVarP(
		&o.Overwrite,
		"overwrite",
		"y",
		false,
		"overwrite existing files in the target directory",
	)
	c.Flags().StringSliceVar(
		&o.WorkshopNames,
		"workshop",
		nil,
		"initial workshop names to scaffold (repeatable)",
	)
	c.Flags().StringVar(
		&o.Title,
		"title",
		"",
		"short title used for generated workshops",
	)
	c.Flags().StringVar(
		&o.Description,
		"description",
		"",
		"longer summary used for generated workshops",
	)
	c.Flags().StringVar(
		&o.Image,
		"image",
		"",
		"name of the workshop base image to use for generated workshops",
	)
	c.Flags().BoolVar(
		&o.WithGitHubAction,
		"with-github-action",
		false,
		"add GitHub action to publish all workshops in the bundle",
	)
	c.Flags().BoolVar(
		&o.WithKubernetesAccess,
		"with-kubernetes-access",
		false,
		"enable kubernetes access in generated workshops",
	)
	c.Flags().BoolVar(
		&o.WithVirtualCluster,
		"with-virtual-cluster",
		false,
		"enable virtual cluster in generated workshops",
	)
	c.Flags().BoolVar(
		&o.WithDockerDaemon,
		"with-docker-daemon",
		false,
		"enable docker daemon in generated workshops",
	)
	c.Flags().BoolVar(
		&o.WithImageRegistry,
		"with-image-registry",
		false,
		"enable image registry in generated workshops",
	)
	c.Flags().BoolVar(
		&o.WithKubernetesConsole,
		"with-kubernetes-console",
		false,
		"enable Kubernetes console in generated workshops",
	)
	c.Flags().BoolVar(
		&o.WithEditor,
		"with-editor",
		true,
		"enable editor in generated workshops",
	)
	c.Flags().BoolVar(
		&o.WithTerminal,
		"with-terminal",
		true,
		"enable terminal in generated workshops",
	)

	return c
}
