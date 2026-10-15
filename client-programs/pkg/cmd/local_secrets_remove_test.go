package cmd

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

// mintCAKeyPairs mints the CA key pair of each ingress domain from the
// cached root CA, as a deploy for that domain does.
func mintCAKeyPairs(t *testing.T, domains ...string) {
	t.Helper()
	for _, domain := range domains {
		cfg := &v1alpha1.EducatesLocalConfig{}
		cfg.Ingress.Domain = domain
		plan, err := localcerts.Resolve(cfg)
		if err != nil {
			t.Fatalf("Resolve %s: %v", domain, err)
		}
		if err := plan.Commit(lineReporter{&bytes.Buffer{}}); err != nil {
			t.Fatalf("Commit %s: %v", domain, err)
		}
	}
}

// stageRootAndOthers caches the root CA educates-ca with the CA key pairs
// minted from it for a.test and b.test, and Secrets that do not belong to
// that root: a trust-only CA certificate, a TLS certificate, registry
// credentials, and a CA key pair minted from another root.
func stageRootAndOthers(t *testing.T) {
	t.Helper()
	if err := (&LocalSecretsAddCaOptions{}).Run(&bytes.Buffer{}, "educates-ca"); err != nil {
		t.Fatalf("add ca: %v", err)
	}
	mintCAKeyPairs(t, "a.test", "b.test")

	other := testpki.NewRootCA()
	otherCA := other.NewIntermediateCA(testpki.WithPermittedDNSDomains("c.test"))
	leaf := other.NewLeaf(testpki.WithDNSNames("*.wildcard.test"))
	for _, s := range []struct {
		name string
		do   func() error
	}{
		{"corporate-ca", func() error {
			return secrets.WriteCachedSecret(secrets.NewCATrustSecret("corporate-ca", other.CertPEM))
		}},
		{"wildcard-tls", func() error {
			return secrets.WriteCachedSecret(secrets.NewTLSSecret("wildcard-tls", leaf.ChainPEM(), leaf.KeyPEM))
		}},
		{"registry", func() error {
			return secrets.WriteCachedSecret(secrets.NewDockerRegistrySecret("registry", "registry.test", "user", "pass", ""))
		}},
		{"c.test-ca-key-pair", func() error {
			return secrets.WriteCachedSecret(secrets.NewCAKeyPairSecret("c.test", "other-ca", otherCA.CertPEM, otherCA.KeyPEM, other.CertPEM))
		}},
	} {
		if err := s.do(); err != nil {
			t.Fatalf("cache %s: %v", s.name, err)
		}
	}

	want := []string{
		"a.test-ca-key-pair", "b.test-ca-key-pair", "c.test-ca-key-pair",
		"corporate-ca", "educates-ca", "educates-ca-root-key-pair",
		"registry", "wildcard-tls",
	}
	if got := cachedSecretNames(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("staged cache holds %v, want %v", got, want)
	}
}

func TestLocalSecretsRemove_Root_RemovesItsTrustSecretKeyPairAndMintedCAKeyPairs(t *testing.T) {
	// Naming the root's CA trust Secret or its CA root key pair removes
	// the whole root.
	for _, name := range []string{"educates-ca", "educates-ca-root-key-pair"} {
		t.Run(name, func(t *testing.T) {
			withSecretsCache(t)
			stageRootAndOthers(t)

			var out bytes.Buffer
			if err := (&LocalSecretsRemoveOptions{}).Run(&out, name); err != nil {
				t.Fatalf("Run: %v", err)
			}

			want := []string{"c.test-ca-key-pair", "corporate-ca", "registry", "wildcard-tls"}
			if got := cachedSecretNames(t); !reflect.DeepEqual(got, want) {
				t.Errorf("cache holds %v after removing the root, want %v", got, want)
			}
			for _, removed := range []string{"educates-ca", "educates-ca-root-key-pair", "a.test-ca-key-pair", "b.test-ca-key-pair"} {
				if !strings.Contains(out.String(), removed) {
					t.Errorf("output does not name the removed %q:\n%s", removed, out.String())
				}
			}
		})
	}
}

func TestLocalSecretsRemove_Root_LetsAnotherRootBeRegistered(t *testing.T) {
	withSecretsCache(t)
	stageRootAndOthers(t)
	supplied := testpki.NewRootCA()
	files := writeFiles(t, supplied.CertPEM, supplied.KeyPEM)
	another := &LocalSecretsAddCaOptions{CertFile: files[0], KeyFile: files[1]}
	if err := another.Run(&bytes.Buffer{}, "mkcert-ca"); err == nil {
		t.Fatal("a second root CA was registered")
	}

	// As the refusal tells the user to do.
	if err := (&LocalSecretsRemoveOptions{}).Run(&bytes.Buffer{}, "educates-ca"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := another.Run(&bytes.Buffer{}, "mkcert-ca"); err != nil {
		t.Fatalf("add ca after removing the root: %v", err)
	}
	if got := assertRootRegistered(t, "mkcert-ca"); !got.Equal(supplied.Cert) {
		t.Errorf("registered root %q, want the supplied %q", got.Subject, supplied.Cert.Subject)
	}
}

func TestLocalSecretsRemove_OtherSecret_RemovesOnlyIt(t *testing.T) {
	for _, name := range []string{"corporate-ca", "registry", "wildcard-tls", "a.test-ca-key-pair"} {
		t.Run(name, func(t *testing.T) {
			withSecretsCache(t)
			stageRootAndOthers(t)
			before := cachedSecretNames(t)

			var out bytes.Buffer
			if err := (&LocalSecretsRemoveOptions{}).Run(&out, name); err != nil {
				t.Fatalf("Run: %v", err)
			}

			var want []string
			for _, n := range before {
				if n != name {
					want = append(want, n)
				}
			}
			if got := cachedSecretNames(t); !reflect.DeepEqual(got, want) {
				t.Errorf("cache holds %v, want %v", got, want)
			}
			if got, want := out.String(), "Removed \""+name+"\" from the local secrets cache.\n"; got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestLocalSecretsRemove_NotCached_IsNotAnError(t *testing.T) {
	withSecretsCache(t)
	stageRootAndOthers(t)
	before := cachedSecretNames(t)

	var out bytes.Buffer
	if err := (&LocalSecretsRemoveOptions{}).Run(&out, "missing"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := cachedSecretNames(t); !reflect.DeepEqual(got, before) {
		t.Errorf("cache holds %v, want it unchanged %v", got, before)
	}
	if out.Len() != 0 {
		t.Errorf("removing nothing printed %q", out.String())
	}
}

func TestLocalSecretsRemove_RootWithoutItsKeyPair_StillRemovesItsMintedCAKeyPairs(t *testing.T) {
	withSecretsCache(t)
	stageRootAndOthers(t)
	// A cache where only the root key pair was deleted, by hand or by
	// an earlier release, keeps CA key pairs minted from the root.
	if err := secrets.RemoveCachedSecret("educates-ca-root-key-pair"); err != nil {
		t.Fatal(err)
	}

	if err := (&LocalSecretsRemoveOptions{}).Run(&bytes.Buffer{}, "educates-ca"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := []string{"c.test-ca-key-pair", "corporate-ca", "registry", "wildcard-tls"}
	if got := cachedSecretNames(t); !reflect.DeepEqual(got, want) {
		t.Errorf("cache holds %v, want %v", got, want)
	}
}
