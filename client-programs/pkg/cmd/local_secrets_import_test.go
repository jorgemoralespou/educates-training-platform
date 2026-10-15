package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	apiv1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// secretsFile writes the Secrets to a temporary YAML file in the shape
// `educates local secrets export` prints, each document preceded by a
// "---" line, and returns its path.
func secretsFile(t *testing.T, objs ...*apiv1.Secret) string {
	t.Helper()
	var buf bytes.Buffer
	for _, s := range objs {
		s = s.DeepCopy()
		s.APIVersion, s.Kind = "v1", "Secret"
		data, err := yaml.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		buf.WriteString("---\n")
		buf.Write(data)
	}
	p := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// rootSecrets returns the CA trust Secret and CA root key pair that
// register root as root CA name.
func rootSecrets(name string, root *testpki.Certificate) []*apiv1.Secret {
	return []*apiv1.Secret{
		secrets.NewCATrustSecret(name, root.CertPEM),
		secrets.NewCARootKeyPairSecret(name, root.CertPEM, root.KeyPEM),
	}
}

// cacheSnapshot reads every file of the local secrets cache: file name to
// content.
func cacheSnapshot(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join(os.Getenv(utils.EducatesCLIDataHomeEnv), "secrets")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(data)
	}
	return files
}

func TestLocalSecretsImport_SecondRoot_RefusedNamingTheExistingOne(t *testing.T) {
	registry := secrets.NewDockerRegistrySecret("registry", "registry.test", "user", "pass", "")
	for name, imported := range map[string][]*apiv1.Secret{
		"another name":  append([]*apiv1.Secret{registry}, rootSecrets("mkcert-ca", testpki.NewRootCA())...),
		"same name":     append([]*apiv1.Secret{registry}, rootSecrets("educates-ca", testpki.NewRootCA())...),
		"key pair only": {registry, secrets.NewCARootKeyPairSecret("mkcert-ca", testpki.NewRootCA().CertPEM, testpki.NewRootCA().KeyPEM)},
	} {
		t.Run(name, func(t *testing.T) {
			withSecretsCache(t)
			if err := (&LocalSecretsAddCaOptions{}).Run(&bytes.Buffer{}, "educates-ca"); err != nil {
				t.Fatalf("add ca: %v", err)
			}
			before := cacheSnapshot(t)

			err := (&LocalSecretsImportOptions{File: secretsFile(t, imported...)}).Run()
			if err == nil {
				t.Fatal("import registered a second root CA")
			}
			for _, want := range []string{`already registered in the local secrets cache: "educates-ca"`, "educates local secrets remove educates-ca"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not mention %q:\n%v", want, err)
				}
			}
			if after := cacheSnapshot(t); !reflect.DeepEqual(after, before) {
				t.Errorf("a refused import changed the cache: %v", keysOf(after))
			}
		})
	}
}

func TestLocalSecretsImport_FileWithTwoRoots_RefusedNamingThem(t *testing.T) {
	withSecretsCache(t)
	imported := append(rootSecrets("educates-ca", testpki.NewRootCA()), rootSecrets("mkcert-ca", testpki.NewRootCA())...)

	err := (&LocalSecretsImportOptions{File: secretsFile(t, imported...)}).Run()
	if err == nil {
		t.Fatal("import registered two root CAs")
	}
	for _, want := range []string{`more than one root CA: "educates-ca", "mkcert-ca"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
	if names := cachedSecretNames(t); len(names) != 0 {
		t.Errorf("a refused import left %v in the cache", names)
	}
}

func TestLocalSecretsImport_RootIntoACacheWithoutOne_Imported(t *testing.T) {
	withSecretsCache(t)
	root := testpki.NewRootCA()
	imported := append(rootSecrets("mkcert-ca", root),
		secrets.NewCATrustSecret("corporate-ca", testpki.NewRootCA().CertPEM),
		secrets.NewDockerRegistrySecret("registry", "registry.test", "user", "pass", ""))

	if err := (&LocalSecretsImportOptions{File: secretsFile(t, imported...)}).Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{"corporate-ca", "mkcert-ca", "mkcert-ca-root-key-pair", "registry"}
	if got := cachedSecretNames(t); !reflect.DeepEqual(got, want) {
		t.Errorf("cache holds %v, want %v", got, want)
	}
	if got := assertRootRegistered(t, "mkcert-ca"); !got.Equal(root.Cert) {
		t.Errorf("imported root is %q, want %q", got.Subject, root.Cert.Subject)
	}
}

func TestLocalSecretsImport_TheCachedRootAgain_Imported(t *testing.T) {
	withSecretsCache(t)
	if err := (&LocalSecretsAddCaOptions{}).Run(&bytes.Buffer{}, "educates-ca"); err != nil {
		t.Fatalf("add ca: %v", err)
	}
	mintCAKeyPairs(t, "a.test")
	// An export of the whole cache, imported back.
	var exported []*apiv1.Secret
	for _, name := range cachedSecretNames(t) {
		exported = append(exported, cachedSecret(t, name))
	}
	exported = append(exported, secrets.NewCATrustSecret("corporate-ca", testpki.NewRootCA().CertPEM))
	file := secretsFile(t, exported...)
	rootBefore := assertRootRegistered(t, "educates-ca")

	if err := (&LocalSecretsImportOptions{File: file}).Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{"a.test-ca-key-pair", "corporate-ca", "educates-ca", "educates-ca-root-key-pair"}
	if got := cachedSecretNames(t); !reflect.DeepEqual(got, want) {
		t.Errorf("cache holds %v, want %v", got, want)
	}
	if rootAfter := assertRootRegistered(t, "educates-ca"); !rootAfter.Equal(rootBefore) {
		t.Error("re-importing the root changed its certificate")
	}
}

func keysOf(m map[string]string) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
