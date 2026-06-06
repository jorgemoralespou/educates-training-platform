package cmd

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

func TestLocalSecretsAddCa_AutoGen_LookupRoundTrip(t *testing.T) {
	t.Setenv("EDUCATES_CLI_DATA_HOME", t.TempDir())

	o := &LocalSecretsAddCaOptions{IngressDomain: "workshop.test"}
	if err := o.Run("workshop.test-ca"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// The auto-generated cache file should be discoverable by the
	// lookup function (the contract the translator depends on).
	got := secrets.LocalCachedSecretForCertificateAuthority("workshop.test")
	if got != "workshop.test-ca" {
		t.Fatalf("LocalCachedSecretForCertificateAuthority = %q, want %q", got, "workshop.test-ca")
	}
}

func TestLocalSecretsAddCa_AutoGen_ProducesUsableCAPEMs(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	o := &LocalSecretsAddCaOptions{IngressDomain: "workshop.test"}
	if err := o.Run("workshop.test-ca"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dataHome, "secrets", "workshop.test-ca.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"type: kubernetes.io/tls",
		"tls.crt:",
		"tls.key:",
		"training.educates.dev/domain: workshop.test",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("cache file missing %q:\n%s", want, s)
		}
	}

	// Pull the base64-encoded PEMs out of the YAML and parse them.
	certPEM := extractB64Value(t, s, "tls.crt:")
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("tls.crt: no PEM block decoded")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	if !cert.IsCA {
		t.Error("cert.IsCA = false, want true (cert-manager requires CA bit set)")
	}
	if cert.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("KeyUsageCertSign missing — cert-manager cannot sign with this CA")
	}

	keyPEM := extractB64Value(t, s, "tls.key:")
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		t.Fatal("tls.key: no PEM block decoded")
	}
	if _, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes); err != nil {
		t.Errorf("ParsePKCS8PrivateKey: %v", err)
	}
}

func TestLocalSecretsAddCa_PartialFlags_Errors(t *testing.T) {
	t.Setenv("EDUCATES_CLI_DATA_HOME", t.TempDir())
	o := &LocalSecretsAddCaOptions{CertFile: "/nonexistent.crt", IngressDomain: "workshop.test"}
	err := o.Run("workshop.test-ca")
	if err == nil {
		t.Fatal("expected error when only --cert is set without --key")
	}
	if !strings.Contains(err.Error(), "--cert and --key must be provided together") {
		t.Errorf("error %q does not mention both-required rule", err)
	}
}

// extractB64Value finds `<prefix> <quoted-base64>` in the YAML body and
// returns the decoded bytes. The k8s YAML marshaller writes base64
// secret data as a single quoted scalar on one line, so a simple
// substring grab works.
func extractB64Value(t *testing.T, body, prefix string) []byte {
	t.Helper()
	idx := strings.Index(body, prefix)
	if idx < 0 {
		t.Fatalf("%q not found in YAML", prefix)
	}
	rest := body[idx+len(prefix):]
	end := strings.IndexAny(rest, "\n")
	if end < 0 {
		end = len(rest)
	}
	val := strings.Trim(strings.TrimSpace(rest[:end]), `"'`)
	decoded, err := base64.StdEncoding.DecodeString(val)
	if err != nil {
		t.Fatalf("base64 decode %q: %v", prefix, err)
	}
	return decoded
}
