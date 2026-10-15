package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var localSecretsSyncExample = `
  # Copy all cached secrets into the cluster:
  educates local secrets sync

  # Copy cached secrets into a specific cluster context:
  educates local secrets sync --context mycluster
`

type LocalSecretsSyncOptions struct {
	KubeconfigOptions
}

func (o *LocalSecretsSyncOptions) Run() error {
	names, err := localcerts.ResolveSync()

	if err != nil {
		return err
	}

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	client, err := clusterConfig.GetClient()

	if err != nil {
		return errors.Wrapf(err, "unable to create Kubernetes client")
	}

	return secrets.SyncLocalCachedSecretsToCluster(client, names)
}

func (p *ProjectInfo) NewLocalSecretsSyncCmd() *cobra.Command {
	var o LocalSecretsSyncOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "sync",
		Short:   "Copy secrets to cluster",
		Example: localSecretsSyncExample,
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run() },
	}

	c.Flags().StringVar(
		&o.Kubeconfig,
		"kubeconfig",
		"",
		"kubeconfig file to use instead of $KUBECONFIG or $HOME/.kube/config",
	)

	c.Flags().StringVar(
		&o.Context,
		"context",
		"",
		"Context to use from Kubeconfig",
	)

	return c
}
