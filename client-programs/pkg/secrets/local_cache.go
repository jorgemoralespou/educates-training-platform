package secrets

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/pkg/errors"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// secretNamePattern is the allowed shape of a local secret name: a
// DNS-label-ish string. Used by ValidateSecretName.
var secretNamePattern = regexp.MustCompile(`^[a-z0-9]([.a-z0-9-]+)?[a-z0-9]$`)

// ValidateSecretName reports whether name is a valid local secret name,
// returning a descriptive error (naming the offending value and the
// allowed pattern) when it is not. Commands call this from their cobra
// Args validator so an invalid name is rejected before any work runs.
func ValidateSecretName(name string) error {
	if !secretNamePattern.MatchString(name) {
		return errors.Errorf("invalid secret name %q (must match %s)", name, secretNamePattern.String())
	}
	return nil
}

// The cache holds private keys and credentials, so it is readable only by
// its owner.
const (
	// cacheDirMode is the mode of the secrets cache directory.
	cacheDirMode os.FileMode = 0o700

	// cacheFileMode is the mode of each file in the secrets cache.
	cacheFileMode os.FileMode = 0o600
)

// CacheDir returns the on-disk secrets cache directory, creating it if it
// does not yet exist. The directory is owner-only: it is created with
// mode 0700, and an existing directory with any other mode, such as one
// created by an earlier release, is changed to 0700. The path is resolved
// at call time so $EDUCATES_CLI_DATA_HOME (and tests using t.Setenv) take
// effect.
func CacheDir() (string, error) {
	dir := secretsCacheDir()
	if err := os.MkdirAll(path.Dir(dir), os.ModePerm); err != nil {
		return "", errors.Wrapf(err, "unable to create secrets cache directory")
	}
	if err := os.Mkdir(dir, cacheDirMode); err != nil && !os.IsExist(err) {
		return "", errors.Wrapf(err, "unable to create secrets cache directory")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", errors.Wrapf(err, "unable to read secrets cache directory")
	}
	if info.Mode().Perm() != cacheDirMode {
		if err := os.Chmod(dir, cacheDirMode); err != nil {
			return "", errors.Wrapf(err, "unable to make secrets cache directory %q readable only by its owner", dir)
		}
	}
	return dir, nil
}

// WriteCachedSecret marshals secret to YAML and writes it to the cache as
// <name>.yaml with mode 0600, replacing any existing file of that name.
// The file is written in full under a temporary name and then renamed, so
// a replaced file never holds partial content or keeps a looser mode.
func WriteCachedSecret(secret *apiv1.Secret) error {
	dir, err := CacheDir()
	if err != nil {
		return err
	}

	secretData, err := json.MarshalIndent(secret, "", "    ")
	if err != nil {
		return errors.Wrap(err, "failed to generate secret data")
	}

	secretData, err = yaml.JSONToYAML(secretData)
	if err != nil {
		return errors.Wrap(err, "failed to generate YAML data")
	}

	secretFilePath := cachedSecretPath(secret.ObjectMeta.Name)
	if err := writeFileOwnerOnly(dir, secretFilePath, secretData); err != nil {
		return errors.Wrapf(err, "unable to write secret file %q", secretFilePath)
	}

	return nil
}

// writeFileOwnerOnly writes data to filePath, in directory dir, with mode
// cacheFileMode, through a temporary file renamed over filePath. The
// temporary name does not end in .yaml, so a leftover is never read as a
// cached secret.
func writeFileOwnerOnly(dir, filePath string, data []byte) error {
	tmp, err := os.CreateTemp(dir, "."+path.Base(filePath)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(cacheFileMode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, filePath)
}

// LoadCachedSecret reads and decodes the cached secret named name. It
// returns a clear "no secret named" error when the secret is not cached,
// so callers can surface that directly.
func LoadCachedSecret(name string) (*apiv1.Secret, error) {
	secretFilePath := cachedSecretPath(name)

	if _, err := os.Stat(secretFilePath); err != nil {
		if os.IsNotExist(err) {
			return nil, errors.Errorf("no secret named %q in the local secrets cache", name)
		}
		return nil, errors.Wrapf(err, "unable to read secret file %q", secretFilePath)
	}

	return decodeCachedSecret(name)
}

// ListCachedSecretNames returns the names (without the .yaml suffix) of all
// secrets in the cache, sorted. It only reads: a cache directory that does
// not exist yet holds no secrets.
func ListCachedSecretNames() ([]string, error) {
	files, err := os.ReadDir(secretsCacheDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read secrets cache directory")
	}

	var names []string
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".yaml") {
			names = append(names, strings.TrimSuffix(f.Name(), ".yaml"))
		}
	}
	sort.Strings(names)

	return names, nil
}

// QuoteNames returns the cached Secret names quoted and comma separated,
// as messages name them.
func QuoteNames(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, ", ")
}

// RemoveCachedSecret deletes the cached secret named name. Removing a
// secret that is not cached is not an error.
func RemoveCachedSecret(name string) error {
	secretFilePath := cachedSecretPath(name)
	if err := os.Remove(secretFilePath); err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "unable to remove secret file %q", secretFilePath)
	}
	return nil
}

// RemoveCachedSecretAndDependents deletes the cached secret named name
// together with the CA material that depends on it, and returns the
// names it deleted, sorted.
//
// A root CA is removed as a whole: naming either its CA trust Secret or
// its CA root key pair deletes both, and every CA key pair minted from
// the root, which is any cached Secret whose RootCAAnnotation names it.
// A cached file that cannot be decoded names no root, so it is left in
// place. Removing a secret that is not cached is not an error.
func RemoveCachedSecretAndDependents(name string) ([]string, error) {
	root := name
	if r, ok := RootCAName(name); ok {
		root = r
	}

	names, err := ListCachedSecretNames()
	if err != nil {
		return nil, err
	}

	// CA key pairs first and the root last, so an interrupted removal
	// leaves a root that can still mint, and running it again finishes.
	var targets []string
	for _, cached := range names {
		if cached == name || cached == root || cached == RootKeyPairName(root) {
			continue
		}
		secret, err := decodeCachedSecret(cached)
		if err != nil {
			continue
		}
		if secret.Annotations[RootCAAnnotation] == root {
			targets = append(targets, cached)
		}
	}
	targets = append(targets, RootKeyPairName(root), root)

	var removed []string
	for _, target := range targets {
		secretFilePath := cachedSecretPath(target)
		err := os.Remove(secretFilePath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return removed, errors.Wrapf(err, "unable to remove secret file %q", secretFilePath)
		}
		removed = append(removed, target)
	}
	sort.Strings(removed)

	return removed, nil
}

// NewTLSSecret builds a kubernetes.io/tls Secret from PEM-encoded cert and
// key bytes. It records no domain: a TLS certificate serves the domains
// of its wildcard DNS names.
func NewTLSSecret(name string, certPEM, keyPEM []byte) *apiv1.Secret {
	return &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Type:       apiv1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		},
	}
}

// Roles of CA material in the local secrets cache. A root CA named NAME
// is cached as two Secrets: its CA trust Secret NAME, which holds only the
// root certificate and is synced to clusters, and its CA root key pair
// NAME-root-key-pair, which never leaves this machine. Each ingress domain
// gets a CA key pair <domain>-ca-key-pair, an intermediate CA minted from
// the root and limited to that domain, which is the only CA key a cluster
// receives.
const (
	// RootKeyPairSuffix ends the name of a CA root key pair.
	RootKeyPairSuffix = "-root-key-pair"

	// CAKeyPairSuffix ends the name of a CA key pair minted for an
	// ingress domain.
	CAKeyPairSuffix = "-ca-key-pair"

	// DomainAnnotation records the ingress domain a cached Secret is
	// for. A CA key pair carries the domain it was minted for.
	DomainAnnotation = "training.educates.dev/domain"

	// RootCAAnnotation records, on a CA key pair, the name of the root
	// CA it was minted from.
	RootCAAnnotation = "training.educates.dev/root-ca"
)

// RootKeyPairName is the name of the CA root key pair of root CA root.
func RootKeyPairName(root string) string {
	return root + RootKeyPairSuffix
}

// RootCAName is the inverse of RootKeyPairName: the name of the root CA
// whose CA root key pair is named keyPair, and false when keyPair is not
// the name of a CA root key pair.
func RootCAName(keyPair string) (string, bool) {
	return strings.CutSuffix(keyPair, RootKeyPairSuffix)
}

// CAKeyPairName is the name of the CA key pair minted for ingress domain
// domain.
func CAKeyPairName(domain string) string {
	return domain + CAKeyPairSuffix
}

// NewCATrustSecret builds a CA trust Secret: an Opaque Secret holding
// only a CA certificate, as ca.crt.
func NewCATrustSecret(name string, certPEM []byte) *apiv1.Secret {
	return &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Type:       apiv1.SecretTypeOpaque,
		Data:       map[string][]byte{"ca.crt": certPEM},
	}
}

// NewCARootKeyPairSecret builds the CA root key pair of root CA root: a
// kubernetes.io/tls Secret named RootKeyPairName(root) holding the root
// certificate as tls.crt and its private key as tls.key.
func NewCARootKeyPairSecret(root string, certPEM, keyPEM []byte) *apiv1.Secret {
	return &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: RootKeyPairName(root)},
		Type:       apiv1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		},
	}
}

// NewCAKeyPairSecret builds the CA key pair for ingress domain domain,
// minted from root CA root: a kubernetes.io/tls Secret named
// CAKeyPairName(domain) holding the CA certificate followed by the root
// certificate as tls.crt, the CA's private key as tls.key and the root
// certificate as ca.crt, annotated with its domain and its root.
func NewCAKeyPairSecret(domain, root string, certPEM, keyPEM, rootPEM []byte) *apiv1.Secret {
	chain := make([]byte, 0, len(certPEM)+len(rootPEM))
	chain = append(chain, certPEM...)
	chain = append(chain, rootPEM...)
	return &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: CAKeyPairName(domain),
			Annotations: map[string]string{
				DomainAnnotation: domain,
				RootCAAnnotation: root,
			},
		},
		Type: apiv1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": chain,
			"tls.key": keyPEM,
			"ca.crt":  rootPEM,
		},
	}
}

// CachedRootCAs returns the names of the root CAs in the local secrets
// cache, sorted: one for each CA root key pair, named as its CA trust
// Secret. It only reads the cache.
func CachedRootCAs() ([]string, error) {
	names, err := ListCachedSecretNames()
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, name := range names {
		if root, ok := RootCAName(name); ok {
			roots = append(roots, root)
		}
	}
	return roots, nil
}

// NewDockerRegistrySecret builds a kubernetes.io/dockerconfigjson Secret
// holding credentials for a single registry server.
func NewDockerRegistrySecret(name, server, username, password, email string) *apiv1.Secret {
	authString := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, password)))

	dockerConfig := map[string]interface{}{
		"auths": map[string]interface{}{
			server: map[string]string{
				"username": username,
				"password": password,
				"email":    email,
				"auth":     authString,
			},
		},
	}

	dockerConfigData, _ := json.Marshal(dockerConfig)

	return &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Type: "kubernetes.io/dockerconfigjson",
		Data: map[string][]byte{
			".dockerconfigjson": dockerConfigData,
		},
	}
}
