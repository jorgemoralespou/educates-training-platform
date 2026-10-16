package cmd

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// secretsCacheDir is the local secrets cache directory of the current
// CLI data home.
func secretsCacheDir() string {
	return filepath.Join(os.Getenv(utils.EducatesCLIDataHomeEnv), "secrets")
}

// assertOwnerOnly checks the local secrets cache directory is 0700 and
// each of the named files in it is 0600.
func assertOwnerOnly(t *testing.T, files ...string) {
	t.Helper()
	dir := secretsCacheDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("cache directory mode = %v, want %v", got, fs.FileMode(0o700))
	}
	if len(files) == 0 {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		t.Fatal("the cache holds no files")
	}
	for _, f := range files {
		info, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode = %v, want %v", f, got, fs.FileMode(0o600))
		}
	}
}

// cacheWriters writes to the local secrets cache through each command
// and step that does.
var cacheWriters = map[string]func(t *testing.T){
	"add ca": func(t *testing.T) {
		if err := (&LocalSecretsAddCaOptions{}).Run(&bytes.Buffer{}, "educates-ca"); err != nil {
			t.Fatal(err)
		}
	},
	"add ca --cert": func(t *testing.T) {
		o := &LocalSecretsAddCaOptions{CertFile: writeFiles(t, testpki.NewRootCA().CertPEM)[0]}
		if err := o.Run(&bytes.Buffer{}, "corporate-ca"); err != nil {
			t.Fatal(err)
		}
	},
	"add tls": func(t *testing.T) {
		leaf := testpki.NewRootCA().NewLeaf(testpki.WithDNSNames("*.workshop.test"))
		files := writeFiles(t, leaf.CertPEM, leaf.KeyPEM)
		if err := (&LocalSecretsAddTlsOptions{CertFile: files[0], KeyFile: files[1]}).Run("wildcard-tls"); err != nil {
			t.Fatal(err)
		}
	},
	"add docker-registry": func(t *testing.T) {
		o := &LocalSecretsAddDockerRegistryOptions{Server: "registry.test", Username: "user", Password: "pass"}
		if err := o.Run("registry"); err != nil {
			t.Fatal(err)
		}
	},
	"import": func(t *testing.T) {
		file := secretsFile(t, rootSecrets("mkcert-ca", testpki.NewRootCA())...)
		if err := (&LocalSecretsImportOptions{File: file}).Run(); err != nil {
			t.Fatal(err)
		}
	},
	"CA key pair minted on deploy": func(t *testing.T) {
		if err := (&LocalSecretsAddCaOptions{}).Run(&bytes.Buffer{}, "educates-ca"); err != nil {
			t.Fatal(err)
		}
		mintCAKeyPairs(t, "workshop.test")
		if _, err := os.Stat(filepath.Join(secretsCacheDir(), "workshop.test-ca-key-pair.yaml")); err != nil {
			t.Fatalf("no CA key pair minted: %v", err)
		}
	},
}

func TestLocalSecretsCache_CreatedOwnerOnly(t *testing.T) {
	for name, write := range cacheWriters {
		t.Run(name, func(t *testing.T) {
			// A data home that does not exist yet.
			t.Setenv(utils.EducatesCLIDataHomeEnv, filepath.Join(t.TempDir(), "educates"))

			write(t)

			assertOwnerOnly(t)
		})
	}
}

func TestLocalSecretsCache_LooserCacheTightenedOnWrite(t *testing.T) {
	for name, write := range cacheWriters {
		t.Run(name, func(t *testing.T) {
			withSecretsCache(t)
			// A cache an earlier release created readable by everyone,
			// holding every file the writer writes.
			write(t)
			dir := secretsCacheDir()
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			var files []string
			for _, e := range entries {
				files = append(files, e.Name())
				if err := os.Chmod(filepath.Join(dir, e.Name()), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			// The one-root rule refuses a second root, so start over
			// on the root the writer registers.
			if _, err := secrets.RemoveCachedSecretAndDependents("educates-ca"); err != nil {
				t.Fatal(err)
			}
			if _, err := secrets.RemoveCachedSecretAndDependents("mkcert-ca"); err != nil {
				t.Fatal(err)
			}

			write(t)

			assertOwnerOnly(t, files...)
		})
	}
}
