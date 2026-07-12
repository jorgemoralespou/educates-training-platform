package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

var localSecretsAddDockerRegistryExample = `
  # Cache credentials for a Docker registry:
  educates local secrets add docker-registry myregistry \
    --docker-server registry.example.com \
    --docker-username user --docker-password pass --docker-email user@example.com
`

type LocalSecretsAddDockerRegistryOptions struct {
	Server   string
	Username string
	Password string
	Email    string
}

func (o *LocalSecretsAddDockerRegistryOptions) Run(name string) error {
	secret := secrets.NewDockerRegistrySecret(name, o.Server, o.Username, o.Password, o.Email)

	return secrets.WriteCachedSecret(secret)
}

func (p *ProjectInfo) NewLocalSecretsAddDockerRegistryCmd() *cobra.Command {
	var o LocalSecretsAddDockerRegistryOptions

	var c = &cobra.Command{
		Args:    secretNameArgs,
		Use:     "docker-registry NAME",
		Short:   "Create a secret for use with a Docker registry",
		Example: localSecretsAddDockerRegistryExample,
		RunE:    func(_ *cobra.Command, args []string) error { return o.Run(args[0]) },
	}

	c.Flags().StringVar(
		&o.Server,
		"docker-server",
		"https://index.docker.io/v1/",
		"server location for docker registry",
	)
	c.Flags().StringVar(
		&o.Username,
		"docker-username",
		"",
		"username for docker registry authentication",
	)
	c.Flags().StringVar(
		&o.Password,
		"docker-password",
		"",
		"password for docker registry authentication",
	)
	c.Flags().StringVar(
		&o.Email,
		"docker-email",
		"",
		"email for docker registry",
	)

	c.MarkFlagsRequiredTogether("docker-username", "docker-password", "docker-email")

	return c
}
