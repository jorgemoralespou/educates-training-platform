package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

func TestLoad_EmptyLocalConfig_AppliesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "local-empty.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	local, ok := cfg.(*v1alpha1.EducatesLocalConfig)
	if !ok {
		t.Fatalf("expected *EducatesLocalConfig, got %T", cfg)
	}

	if got, want := local.Cluster.ListenAddress, "127.0.0.1"; got != want {
		t.Errorf("ListenAddress = %q, want %q", got, want)
	}
	if local.ClusterAdmin == nil || *local.ClusterAdmin != true {
		t.Errorf("ClusterAdmin = %v, want true", local.ClusterAdmin)
	}
	if local.LookupService == nil || *local.LookupService != true {
		t.Errorf("LookupService = %v, want true", local.LookupService)
	}
	if local.ImagePrePuller == nil || *local.ImagePrePuller != false {
		t.Errorf("ImagePrePuller = %v, want false", local.ImagePrePuller)
	}
	if got, want := local.Operator.LogLevel, "info"; got != want {
		t.Errorf("Operator.LogLevel = %q, want %q", got, want)
	}
}

func TestLoad_FullLocalConfig_RoundTripsAllFields(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "local-full.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	local := cfg.(*v1alpha1.EducatesLocalConfig)

	if got, want := local.Cluster.ListenAddress, "192.168.1.10"; got != want {
		t.Errorf("ListenAddress = %q, want %q", got, want)
	}
	if got, want := local.Cluster.ApiServer.Port, 6443; got != want {
		t.Errorf("ApiServer.Port = %d, want %d", got, want)
	}
	if got, want := len(local.Cluster.VolumeMounts), 1; got != want {
		t.Fatalf("VolumeMounts len = %d, want %d", got, want)
	}
	if got, want := local.Cluster.VolumeMounts[0].HostPath, "/tmp/data"; got != want {
		t.Errorf("VolumeMounts[0].HostPath = %q, want %q", got, want)
	}
	if local.ClusterAdmin == nil || *local.ClusterAdmin != false {
		t.Errorf("ClusterAdmin = %v, want false (explicit override)", local.ClusterAdmin)
	}
	if got, want := local.Operator.LogLevel, "debug"; got != want {
		t.Errorf("Operator.LogLevel = %q, want %q", got, want)
	}
	if got, want := local.Ingress.Domain, "workshop.test"; got != want {
		t.Errorf("Ingress.Domain = %q, want %q", got, want)
	}
	if got, want := len(local.Resolver.ExtraDomains), 2; got != want {
		t.Errorf("ExtraDomains len = %d, want %d", got, want)
	}
	if got, want := local.WebsiteStyling.DefaultTheme, "educates-default"; got != want {
		t.Errorf("WebsiteStyling.DefaultTheme = %q, want %q", got, want)
	}
}

func TestLoad_Errors(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		contains string
	}{
		{"unknown-field",   "local-unknown-field.yaml",  "bogusField"},
		{"bad-enum",        "local-bad-loglevel.yaml",   "logLevel"},
		{"wrong-apiVersion","wrong-apiversion.yaml",     "unsupported apiVersion"},
		{"unknown-kind",    "unknown-kind.yaml",         "unknown kind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(filepath.Join("testdata", tc.file))
			if err == nil {
				t.Fatalf("Load: expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.contains) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.contains)
			}
		})
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join("testdata", "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("Load: expected error for missing file")
	}
}

func TestLoadLocal_RejectsNonLocalKind(t *testing.T) {
	_, err := LoadLocal(filepath.Join("testdata", "escape-minimal.yaml"))
	if err == nil {
		t.Fatal("LoadLocal: expected error for EducatesConfig kind")
	}
	if !strings.Contains(err.Error(), "expected kind") {
		t.Errorf("error %q does not mention expected kind", err.Error())
	}
}

func TestLoad_EducatesConfig_Minimal(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "escape-minimal.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	esc, ok := cfg.(*v1alpha1.EducatesConfig)
	if !ok {
		t.Fatalf("expected *EducatesConfig, got %T", cfg)
	}
	if esc.Target != nil {
		t.Errorf("Target = %+v, want nil", esc.Target)
	}
}

func TestLoad_EducatesConfig_WithTarget(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "escape-with-target.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	esc := cfg.(*v1alpha1.EducatesConfig)
	if esc.Target == nil {
		t.Fatal("Target = nil, want populated")
	}
	if got, want := esc.Target.Provider, "kind"; got != want {
		t.Errorf("Target.Provider = %q, want %q", got, want)
	}
	if got, want := esc.Operator.LogLevel, "debug"; got != want {
		t.Errorf("Operator.LogLevel = %q, want %q (no defaulting for escape kind)", got, want)
	}
	if esc.SecretsManager == nil {
		t.Errorf("SecretsManager = nil, want empty map")
	}
}

func TestLoad_EducatesInlineConfig_Minimal(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "inline-minimal.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	inline, ok := cfg.(*v1alpha1.EducatesInlineConfig)
	if !ok {
		t.Fatalf("expected *EducatesInlineConfig, got %T", cfg)
	}
	if got, want := inline.Domain, "workshop.test"; got != want {
		t.Errorf("Domain = %q, want %q", got, want)
	}
	// Defaults applied.
	if got, want := inline.Operator.LogLevel, "info"; got != want {
		t.Errorf("Operator.LogLevel = %q, want %q", got, want)
	}
	if got, want := inline.PolicyEnforcement.ClusterEngine, "Kyverno"; got != want {
		t.Errorf("PolicyEnforcement.ClusterEngine default = %q, want %q", got, want)
	}
}

func TestLoad_EducatesInlineConfig_MissingRequired(t *testing.T) {
	cfg := []byte("apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesInlineConfig\n")
	_, err := LoadBytes(cfg, "test")
	if err == nil {
		t.Fatal("expected error for missing required fields")
	}
	// Schema should call out one of the required fields.
	for _, want := range []string{"domain", "ingressClassName", "wildcardCertificateSecret"} {
		if strings.Contains(err.Error(), want) {
			return
		}
	}
	t.Errorf("error %q does not mention any required Inline field", err)
}

func TestLoad_EducatesConfig_BogusEnvelopeField(t *testing.T) {
	_, err := Load(filepath.Join("testdata", "escape-bogus-envelope-field.yaml"))
	if err == nil {
		t.Fatal("Load: expected error for unknown envelope field")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error %q does not mention bogus field", err.Error())
	}
}
