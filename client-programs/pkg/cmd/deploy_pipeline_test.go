package cmd

import (
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// TestLocalCASecretIfSecure covers the shared fail-fast check used by both
// the deploy pipeline and the cluster-create preflight: an insecure
// install needs no CA, a secure install without a cached CA errors, and a
// secure install with a cached CA resolves the ref.
func TestLocalCASecretIfSecure(t *testing.T) {
	t.Run("insecure skips the lookup", func(t *testing.T) {
		// A domain is set and no CA is cached, but insecure means no CA
		// is needed, so the lookup must be skipped entirely.
		t.Setenv("EDUCATES_CLI_DATA_HOME", t.TempDir())
		cfg := &v1alpha1.EducatesLocalConfig{}
		cfg.Ingress.Domain = "workshop.test"
		cfg.Ingress.Insecure = true
		name, ns, err := localCASecretIfSecure(cfg)
		if err != nil || name != "" || ns != "" {
			t.Fatalf("got (%q, %q, %v), want empty name+namespace and nil error", name, ns, err)
		}
	})

	t.Run("secure without a cached CA errors", func(t *testing.T) {
		t.Setenv("EDUCATES_CLI_DATA_HOME", t.TempDir())
		cfg := &v1alpha1.EducatesLocalConfig{}
		cfg.Ingress.Domain = "workshop.test"
		if _, _, err := localCASecretIfSecure(cfg); err == nil {
			t.Fatal("expected an error for a secure config with no cached CA")
		}
	})

	t.Run("secure with a cached CA returns the ref", func(t *testing.T) {
		_, caName := withCachedCA(t, "workshop.test")
		cfg := &v1alpha1.EducatesLocalConfig{}
		cfg.Ingress.Domain = "workshop.test"
		name, ns, err := localCASecretIfSecure(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if name != caName || ns != LocalCASecretNamespace {
			t.Errorf("got (%q, %q), want (%q, %q)", name, ns, caName, LocalCASecretNamespace)
		}
	})
}
