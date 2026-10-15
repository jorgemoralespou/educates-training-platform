package secrets

import (
	"context"
	"os"
	"path"

	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	apiv1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/scheme"
)

// secretsCacheDir resolves the on-disk secrets cache directory at call
// time rather than at package-init, so $EDUCATES_CLI_DATA_HOME (and
// tests using t.Setenv) takes effect.
func secretsCacheDir() string {
	return path.Join(utils.GetEducatesHomeDir(), "secrets")
}

// CachedSecretPath is the file the cached Secret name is stored in.
func CachedSecretPath(name string) string {
	return path.Join(secretsCacheDir(), name+".yaml")
}

// LocalSecretsSyncNamespace is the cluster namespace the local secrets
// cache is synced into.
const LocalSecretsSyncNamespace = "educates-secrets"

// SyncLocalCachedSecretsToCluster ensures the LocalSecretsSyncNamespace
// namespace exists and copies the named Secrets from the local cache into
// it, creating or updating each one.
func SyncLocalCachedSecretsToCluster(client *kubernetes.Clientset, names []string) error {
	namespacesClient := client.CoreV1().Namespaces()

	_, err := namespacesClient.Get(context.TODO(), LocalSecretsSyncNamespace, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		namespaceObj := apiv1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: LocalSecretsSyncNamespace,
			},
		}

		// Surface RBAC / admission / transient errors here rather than
		// proceeding to write Secrets into a namespace that doesn't
		// exist — the subsequent secret writes would fail with a
		// confusing 'namespaces "educates-secrets" not found' that
		// masks the real cause (typically: kubeconfig user lacks
		// 'create namespaces').
		if _, err := namespacesClient.Create(context.TODO(), &namespaceObj, metav1.CreateOptions{}); err != nil && !k8serrors.IsAlreadyExists(err) {
			return errors.Wrapf(err, "unable to create namespace %q for local secrets sync", LocalSecretsSyncNamespace)
		}
	} else if err != nil {
		return errors.Wrapf(err, "unable to check namespace %q for local secrets sync", LocalSecretsSyncNamespace)
	}

	secretsClient := client.CoreV1().Secrets(LocalSecretsSyncNamespace)

	for _, name := range names {
		secretObj, err := decodeCachedSecret(name)
		if err != nil {
			return err
		}

		secretObj.ObjectMeta.Namespace = ""

		_, err = secretsClient.Get(context.TODO(), name, metav1.GetOptions{})

		// Create the secret if it doesn't exist.
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return errors.Wrap(err, "unable to read secrets from cluster")
			} else {
				_, err = secretsClient.Create(context.TODO(), secretObj, metav1.CreateOptions{})

				if err != nil {
					return errors.Wrapf(err, "unable to copy secret to cluster %q", name)
				}
			}
			// Update the secret if it does exist.
		} else {
			var patch *applycorev1.SecretApplyConfiguration

			if len(secretObj.StringData) != 0 {
				patch = applycorev1.Secret(name, LocalSecretsSyncNamespace).WithType(secretObj.Type).WithStringData(secretObj.StringData)
			} else {
				patch = applycorev1.Secret(name, LocalSecretsSyncNamespace).WithType(secretObj.Type).WithData(secretObj.Data)
			}

			_, err = secretsClient.Apply(context.TODO(), patch, metav1.ApplyOptions{FieldManager: "educates-cli", Force: true})

			if err != nil {
				return errors.Wrapf(err, "unable to update secret in cluster %q", name)
			}
		}
	}

	return nil
}

// decodeCachedSecret reads and decodes the cached Secret name.
func decodeCachedSecret(name string) (*apiv1.Secret, error) {
	fullPath := CachedSecretPath(name)

	yamlData, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read secret file %q", fullPath)
	}

	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDecoder()
	secretObj := &apiv1.Secret{}
	err = runtime.DecodeInto(decoder, yamlData, secretObj)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read secret file %q", fullPath)
	}
	return secretObj, nil
}
