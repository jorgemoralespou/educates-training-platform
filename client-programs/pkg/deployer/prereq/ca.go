// Package prereq checks deploy prerequisites that must be satisfied before
// the four platform CRs can reconcile.
package prereq

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

// CustomCASecretName is the Secret the Local-mode translator hardcodes
// as caCertificateRef. Must exist in the operator namespace before deploy
// applies EducatesClusterConfig.
const CustomCASecretName = "educates-custom-ca"

// CheckCustomCASecret returns nil when the educates-custom-ca Secret
// exists in the operator namespace, or a user-actionable error pointing
// at the kubectl command that creates it.
func CheckCustomCASecret(ctx context.Context, getter genericclioptions.RESTClientGetter, namespace string) error {
	cfg, err := getter.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("REST config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("kubernetes client: %w", err)
	}
	_, err = cs.CoreV1().Secrets(namespace).Get(ctx, CustomCASecretName, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("read Secret %s/%s: %w", namespace, CustomCASecretName, err)
	}
	return fmt.Errorf(`missing prerequisite: Secret %q in namespace %q.

EducatesLocalConfig deploys cert-manager in CustomCA mode, which signs
the cluster's wildcard TLS cert from a CA you provide. Create the Secret
before re-running deploy:

  kubectl create namespace %s
  kubectl -n %s create secret tls %s \
    --cert=ca.crt \
    --key=ca.key

For development on a laptop, a self-signed CA is fine. Generate one with:

  openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
    -subj '/CN=educates-dev-ca' \
    -keyout ca.key -out ca.crt`,
		CustomCASecretName, namespace,
		namespace, namespace, CustomCASecretName)
}
