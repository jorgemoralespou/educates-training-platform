package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
)

const (
	emptyLocal = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
`
	localWithDomain = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
ingress:
  domain: workshop.test
operator:
  image:
    repository: ghcr.io/educates/educates-operator
    tag: 4.0.0
`
	escapeMinimal = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesConfig
`
)

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// stageCachedCA drops a synthetic CA Secret YAML into <dataHome>/secrets/
// with the 'training.educates.dev/domain' annotation set to `domain`,
// matching the v4 lookup criteria (kubernetes.io/tls type with both
// tls.crt and tls.key data). The byte contents are placeholders — the
// test only exercises the lookup-by-domain path, not PEM validity.
func stageCachedCA(t *testing.T, dataHome, domain string) (caName string) {
	t.Helper()
	caName = "test-ca"
	secretsDir := filepath.Join(dataHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "apiVersion: v1\nkind: Secret\nmetadata:\n  name: " + caName +
		"\n  annotations:\n    training.educates.dev/domain: " + domain +
		"\ntype: kubernetes.io/tls\ndata:\n  tls.crt: dGVzdC1jcnQ=\n  tls.key: dGVzdC1rZXk=\n"
	if err := os.WriteFile(filepath.Join(secretsDir, caName+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return
}

// withCachedCA is a convenience wrapper: creates a fresh temp data home,
// stages a CA for `domain`, and sets $EDUCATES_CLI_DATA_HOME. Use it
// when the test doesn't already manage its own data home.
func withCachedCA(t *testing.T, domain string) (dataHome, caName string) {
	t.Helper()
	dataHome = t.TempDir()
	caName = stageCachedCA(t, dataHome, domain)
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	return
}

func TestRender_Config_MissingDomain_Errors(t *testing.T) {
	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	o := &PlatformRenderOptions{Config: writeFixture(t, emptyLocal)}

	var buf bytes.Buffer
	err := p.runRender(&buf, o)
	if err == nil {
		t.Fatal("expected error for missing ingress.domain in --config mode")
	}
	if !strings.Contains(err.Error(), "ingress.domain is required") {
		t.Errorf("error %q does not mention required field", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output when erroring, got %d bytes", buf.Len())
	}
}

func TestRender_Config_WithDomain_NoHostHeader(t *testing.T) {
	withCachedCA(t, "workshop.test")
	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	o := &PlatformRenderOptions{Config: writeFixture(t, localWithDomain)}

	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	s := buf.String()

	// No host-derivation note (user-provided domain).
	if strings.Contains(s, "auto-derived from host IP") {
		t.Errorf("--config mode should not emit host-defaulting note:\n%s", s)
	}
	// Both sections present.
	for _, want := range []string{
		"# === operator chart values",
		"# === platform CRs",
		"domain: workshop.test",
		"tag: 4.0.0",
		"repository: ghcr.io/educates/educates-operator",
		"kind: EducatesClusterConfig",
		"kind: SecretsManager",
		"kind: SessionManager",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRender_Config_CLIDefaults_AppliedWhenImageEmpty(t *testing.T) {
	// Tag and repository come from ProjectInfo when config leaves them empty.
	withCachedCA(t, "workshop.test")
	cfgYAML := `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
ingress:
  domain: workshop.test
`
	p := ProjectInfo{Version: "v9.9.9-test", ImageRepository: "ghcr.io/custom"}
	o := &PlatformRenderOptions{Config: writeFixture(t, cfgYAML)}

	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	s := buf.String()
	for _, want := range []string{
		"tag: v9.9.9-test",
		"repository: ghcr.io/custom/educates-operator",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("CLI defaults: output missing %q:\n%s", want, s)
		}
	}
}

func TestRender_LocalConfig_AutoDomain_DefaultsInsecure(t *testing.T) {
	// Point --local-config at a temp data home so the test doesn't touch
	// the user's actual data home.
	dataHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(emptyLocal), 0o644); err != nil {
		t.Fatal(err)
	}
	// No CA is staged: an empty local config falls back to a nip.io
	// domain, which defaults to an insecure plain-HTTP install needing no
	// CA. maybeApplyHostDomain needs a detectable host IP to fill the
	// domain.
	if _, err := hostinfo.DetectHostIP(); err != nil {
		t.Skipf("no host IP detectable: %v", err)
	}
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	o := &PlatformRenderOptions{LocalConfig: true}

	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	s := buf.String()
	if !strings.Contains(s, "auto-derived from host IP") {
		t.Errorf("--local-config with empty domain should emit host-defaulting note:\n%s", s)
	}
	if !strings.Contains(s, ".nip.io") {
		t.Errorf("--local-config with empty domain should produce a nip.io domain:\n%s", s)
	}
	if !strings.Contains(s, "provider: None") {
		t.Errorf("--local-config with empty domain should default to the None certificates provider:\n%s", s)
	}
	if !strings.Contains(s, "protocol: http") {
		t.Errorf("--local-config with empty domain should default to http protocol:\n%s", s)
	}
}

func TestRender_LocalConfig_UserDomain_NoHeader(t *testing.T) {
	dataHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(localWithDomain), 0o644); err != nil {
		t.Fatal(err)
	}
	stageCachedCA(t, dataHome, "workshop.test")
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	o := &PlatformRenderOptions{LocalConfig: true}

	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	if strings.Contains(buf.String(), "auto-derived from host IP") {
		t.Errorf("--local-config with explicit domain should not emit host-defaulting note:\n%s", buf.String())
	}
}

func TestRender_EscapeKind_PureUserOutput(t *testing.T) {
	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	o := &PlatformRenderOptions{Config: writeFixture(t, escapeMinimal)}

	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	s := buf.String()

	// Escape kind: no CLI defaulting. The operator chart values section
	// should NOT contain the CLI's projectVersion-derived tag — the
	// fixture didn't declare operator.image, so output is empty.
	if strings.Contains(s, "tag: test") {
		t.Errorf("escape kind should not apply CLI image defaults:\n%s", s)
	}
	if !strings.Contains(s, "kind: EducatesClusterConfig") {
		t.Errorf("escape kind should still emit CR wrappers:\n%s", s)
	}
}
