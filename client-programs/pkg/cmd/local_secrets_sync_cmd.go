package cmd

import (
	"fmt"
	"io"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var localSecretsSyncExample = `
  # Copy the cached secrets into the cluster:
  educates local secrets sync

  # Copy cached secrets into a specific cluster context:
  educates local secrets sync --context mycluster
`

type LocalSecretsSyncOptions struct {
	KubeconfigOptions
}

// Run pushes the cached Secrets the local certificate resolution step
// selects into the cluster. CA root key pairs are never pushed; a CA key
// pair is pushed only when the local config selects it, and is minted
// first when it is missing or stale. When the certificates of the local
// config cannot be decided, it warns and pushes every Secret that holds
// no CA key.
func (o *LocalSecretsSyncOptions) Run(w io.Writer) error {
	plan, err := localcerts.ResolveSync()

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

	if err := plan.Commit(lineReporter{w}); err != nil {
		return err
	}

	return secrets.SyncLocalCachedSecretsToCluster(client, plan.SyncSecretNames)
}

// lineReporter is a localcerts.Reporter that prints each note as a line.
type lineReporter struct{ w io.Writer }

func (r lineReporter) Note(msg string) {
	fmt.Fprintln(r.w, msg)
}

func (p *ProjectInfo) NewLocalSecretsSyncCmd() *cobra.Command {
	var o LocalSecretsSyncOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "sync",
		Short: "Copy secrets to cluster",
		Long: `Copy the cached secrets into the educates-secrets namespace of the cluster.

CA root key pairs are never copied. A CA key pair is copied only when the
local config's ingress.domain selects it, and is minted first from the
root CA when it is missing or stale. Without a local config, no CA key
pair is copied. When the local config's certificates cannot be decided,
such as a secure ingress.domain with neither a root CA nor a wildcard
certificate for it cached, the command warns why and copies every secret
that holds no CA key.`,
		Example: localSecretsSyncExample,
		RunE:    func(cmd *cobra.Command, _ []string) error { return o.Run(cmd.OutOrStdout()) },
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
