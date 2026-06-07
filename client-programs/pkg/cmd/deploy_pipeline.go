package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
)

// deployPipelineFlags collects the kubectl/helm connection flags shared
// by both admin platform deploy and local cluster create's tail-call.
// Extracted so the two cmd paths can't drift on which flags they plumb
// (the original drift was tailCallDeploy missing --context).
type deployPipelineFlags struct {
	Kubeconfig string
	Context    string
	Timeout    time.Duration
	Verbose    bool
}

// translateAndDeploy is the shared "config → translator output → deploy"
// tail. Both admin platform deploy (after load+default) and local
// cluster create (after kind+registry bring-up) call this with a
// fully-loaded+defaulted config. Keeps the deploy.Options + Getter
// construction in one place so a new flag or default reaches both.
//
// caSecret{Name,Namespace} are required when cfg is *EducatesLocalConfig
// and ignored otherwise (the dispatcher in translator.Translate routes
// the opts only to TranslateLocal today).
func translateAndDeploy(
	ctx context.Context,
	w io.Writer,
	cfg v1alpha1.Config,
	caSecretName, caSecretNamespace string,
	syncLocalSecrets bool,
	flags deployPipelineFlags,
) error {
	out, err := translator.Translate(cfg, translator.Options{
		CASecretName:      caSecretName,
		CASecretNamespace: caSecretNamespace,
	})
	if err != nil {
		return err
	}

	cf := genericclioptions.NewConfigFlags(true)
	if flags.Kubeconfig != "" {
		cf.KubeConfig = &flags.Kubeconfig
	}
	if flags.Context != "" {
		cf.Context = &flags.Context
	}
	ns := deployer.OperatorNamespace
	cf.Namespace = &ns

	helmLog := io.Discard
	if flags.Verbose {
		helmLog = w
	}

	return deployer.Deploy(ctx, out, deployer.Options{
		Getter:           cf,
		Out:              w,
		HelmLog:          helmLog,
		Timeout:          flags.Timeout,
		SyncLocalSecrets: syncLocalSecrets,
	})
}

// caRefForLocal looks up the cached CA Secret name + the conventional
// 'educates-secrets' namespace for an EducatesLocalConfig at translate
// time. Returns ("", "", err) when no cached CA matches the domain.
// Caller has already applied host-IP defaulting so c.Ingress.Domain is
// non-empty when we get here.
func caRefForLocal(c *v1alpha1.EducatesLocalConfig) (name, namespace string, err error) {
	if c.Ingress.Domain == "" {
		return "", "", fmt.Errorf("internal: ingress.domain is empty at CA-lookup time (host-IP defaulting should have run)")
	}
	name, err = lookupLocalCAByDomain(c.Ingress.Domain)
	if err != nil {
		return "", "", err
	}
	return name, LocalCASecretNamespace, nil
}
