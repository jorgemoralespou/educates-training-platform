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
	// unknown-kind.yaml is rejected at the discriminator stage, so use
	// LoadBytes with a kind we'll register later to exercise the type check.
	// For now LoadLocal will surface the same "unknown kind" path.
	_, err := LoadLocal(filepath.Join("testdata", "unknown-kind.yaml"))
	if err == nil {
		t.Fatal("LoadLocal: expected error")
	}
}
