package secrets_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

// withSecretsCache points the CLI data home, and so the local secrets
// cache, at a fresh temporary directory, and returns the cache directory.
func withSecretsCache(t *testing.T) string {
	t.Helper()
	t.Setenv("EDUCATES_CLI_DATA_HOME", t.TempDir())
	dir, err := secrets.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestInvalidCachedSecret_ReasonTellsAnAlphaCAFromAnUnreadableFile(t *testing.T) {
	dir := withSecretsCache(t)
	ca := testpki.NewRootCA()
	// A CA cached with its key under a plain name, as 4.0.0 alpha
	// releases cached one.
	if err := secrets.WriteCachedSecret(secrets.NewTLSSecret("workshop.test-ca", ca.CertPEM, ca.KeyPEM)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.yaml"), []byte("kind: Secret\ndata: [not a map\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s, role, invalid, err := secrets.LoadClassifiedCachedSecret("workshop.test-ca")
	if err != nil {
		t.Fatalf("load the alpha CA: %v", err)
	}
	if s == nil || role != secrets.InvalidRole {
		t.Errorf("alpha CA loaded as %q with Secret %v, want %q with its Secret", role, s, secrets.InvalidRole)
	}
	if invalid == nil || invalid.Problem != secrets.CachedSecretUnmanagedCAKey {
		t.Fatalf("alpha CA invalid because %v, want an unmanaged CA key", invalid)
	}
	for _, want := range []string{`"workshop.test-ca"`, "private key", "4.0.0 alpha"} {
		if !strings.Contains(invalid.Error(), want) {
			t.Errorf("reason %q does not mention %q", invalid, want)
		}
	}

	s, role, invalid, err = secrets.LoadClassifiedCachedSecret("broken")
	if err != nil {
		t.Fatalf("load the broken file: %v", err)
	}
	if s != nil || role != secrets.InvalidRole {
		t.Errorf("broken file loaded as %q with Secret %v, want %q without one", role, s, secrets.InvalidRole)
	}
	if invalid == nil || invalid.Problem != secrets.CachedSecretUnreadable {
		t.Fatalf("broken file invalid because %v, want an unreadable cached secret", invalid)
	}
	if !strings.Contains(invalid.Error(), "broken.yaml") {
		t.Errorf("reason %q does not name the file", invalid)
	}

	var unreadable *secrets.InvalidCachedSecretError
	if _, err := secrets.LoadCachedSecret("broken"); !errors.As(err, &unreadable) || unreadable.Problem != secrets.CachedSecretUnreadable {
		t.Errorf("loading the broken file failed with %v, want an unreadable cached secret", err)
	}
}

func TestInvalidCachedSecret_NoReasonForAUsableSecretOrAMissingOne(t *testing.T) {
	withSecretsCache(t)
	root := testpki.NewRootCA()
	leaf := root.NewLeaf(testpki.WithDNSNames("*.workshop.test"))
	for _, s := range []struct {
		name   string
		secret func() error
	}{
		{"educates-ca", func() error { return secrets.WriteCachedSecret(secrets.NewCATrustSecret("educates-ca", root.CertPEM)) }},
		{"educates-ca-root-key-pair", func() error {
			return secrets.WriteCachedSecret(secrets.NewCARootKeyPairSecret("educates-ca", root.CertPEM, root.KeyPEM))
		}},
		{"workshop-tls", func() error {
			return secrets.WriteCachedSecret(secrets.NewTLSSecret("workshop-tls", leaf.CertPEM, leaf.KeyPEM))
		}},
	} {
		if err := s.secret(); err != nil {
			t.Fatal(err)
		}
		_, role, invalid, err := secrets.LoadClassifiedCachedSecret(s.name)
		if err != nil {
			t.Fatalf("load %s: %v", s.name, err)
		}
		if role == secrets.InvalidRole || invalid != nil {
			t.Errorf("%s is %q because %v, want it valid", s.name, role, invalid)
		}
	}

	_, _, _, err := secrets.LoadClassifiedCachedSecret("missing")
	var invalid *secrets.InvalidCachedSecretError
	if err == nil || errors.As(err, &invalid) {
		t.Errorf("loading a name the cache does not hold failed with %v, want a plain error", err)
	}
}
