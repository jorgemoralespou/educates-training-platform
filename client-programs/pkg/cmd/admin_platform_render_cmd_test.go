package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestRender_LocalConfig_AutoDomain_EmitsHeader(t *testing.T) {
	// Point --local-config at a temp data home so the test doesn't touch
	// the user's actual ~/.educates.
	dataHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(emptyLocal), 0o644); err != nil {
		t.Fatal(err)
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
}

func TestRender_LocalConfig_UserDomain_NoHeader(t *testing.T) {
	dataHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(localWithDomain), 0o644); err != nil {
		t.Fatal(err)
	}
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
