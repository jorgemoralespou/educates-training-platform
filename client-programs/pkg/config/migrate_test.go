package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const v3KindValues = `
clusterInfrastructure:
  provider: kind
clusterIngress:
  domain: educates.test
localKindCluster:
  listenAddress: 192.168.1.10
  apiServer:
    address: 192.168.1.10
    port: 6443
  networking:
    serviceSubnet: 10.96.0.0/12
    podSubnet: 10.244.0.0/16
  volumeMounts:
    - hostPath: /tmp/data
      containerPath: /data
      readOnly: true
  registryMirrors:
    - mirror: docker.io
      url: https://proxy.local
localDNSResolver:
  targetAddress: 192.168.1.10
  extraDomains:
    - example.test
imageVersions:
  - name: "1.0"
    image: ghcr.io/educates/example:1.0
websiteStyling:
  defaultTheme: educates-default
  themeDataRefs:
    - namespace: educates
      name: my-theme-data
secretPropagation:
  imagePullSecretNames:
    - my-pull-secret
clusterSecurity:
  policyEngine: pod-security-standards
`

func TestMaybeMigrateV3_NoState_Noop(t *testing.T) {
	dataHome := t.TempDir()
	if err := MaybeMigrateV3(dataHome); err != nil {
		t.Fatalf("MaybeMigrateV3: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "config.yaml")); err == nil {
		t.Error("config.yaml should not have been created from empty data home")
	}
}

func TestMaybeMigrateV3_ConfigYAMLPresent_Noop(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte("existing"), 0o644))
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"), []byte(v3KindValues), 0o644))

	if err := MaybeMigrateV3(dataHome); err != nil {
		t.Fatalf("MaybeMigrateV3: %v", err)
	}

	body := readFile(t, filepath.Join(dataHome, "config.yaml"))
	if string(body) != "existing" {
		t.Errorf("config.yaml content overwritten: %q", body)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "values.yaml.v3-backup")); err == nil {
		t.Error("v3 backup should not have been created when config.yaml already present")
	}
}

func TestMaybeMigrateV3_KindProvider_Migrates(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"), []byte(v3KindValues), 0o644))

	if err := MaybeMigrateV3(dataHome); err != nil {
		t.Fatalf("MaybeMigrateV3: %v", err)
	}

	// values.yaml renamed.
	if _, err := os.Stat(filepath.Join(dataHome, "values.yaml")); err == nil {
		t.Error("values.yaml should have been renamed")
	}
	if _, err := os.Stat(filepath.Join(dataHome, "values.yaml.v3-backup")); err != nil {
		t.Errorf("values.yaml.v3-backup missing: %v", err)
	}

	// config.yaml should load cleanly via the v4 loader.
	cfgPath := filepath.Join(dataHome, "config.yaml")
	cfg, err := LoadLocal(cfgPath)
	if err != nil {
		t.Fatalf("LoadLocal on migrated file: %v", err)
	}

	// Spot-check a representative scattering of fields across the
	// translation table; full coverage is the schema's job at load.
	if got, want := cfg.Ingress.Domain, "educates.test"; got != want {
		t.Errorf("Ingress.Domain = %q, want %q", got, want)
	}
	if got, want := cfg.Cluster.ApiServer.Port, 6443; got != want {
		t.Errorf("Cluster.ApiServer.Port = %d, want %d", got, want)
	}
	if got := len(cfg.Cluster.VolumeMounts); got != 1 {
		t.Fatalf("VolumeMounts len = %d, want 1", got)
	}
	if cfg.Cluster.VolumeMounts[0].ReadOnly == nil || !*cfg.Cluster.VolumeMounts[0].ReadOnly {
		t.Errorf("VolumeMounts[0].ReadOnly = %v, want true", cfg.Cluster.VolumeMounts[0].ReadOnly)
	}
	if got, want := cfg.Resolver.TargetAddress, "192.168.1.10"; got != want {
		t.Errorf("Resolver.TargetAddress = %q, want %q", got, want)
	}
	if got := len(cfg.Resolver.ExtraDomains); got != 1 {
		t.Errorf("ExtraDomains len = %d, want 1", got)
	}
	if got, want := cfg.WebsiteStyling.DefaultTheme, "educates-default"; got != want {
		t.Errorf("DefaultTheme = %q, want %q", got, want)
	}
	if got, want := len(cfg.WebsiteStyling.ThemeDataRefs), 1; got != want {
		t.Errorf("ThemeDataRefs len = %d, want %d", got, want)
	}
}

func TestMaybeMigrateV3_EmptyProvider_Migrates(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"),
		[]byte("clusterIngress:\n  domain: workshop.test\n"), 0o644))

	if err := MaybeMigrateV3(dataHome); err != nil {
		t.Fatalf("MaybeMigrateV3: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "config.yaml")); err != nil {
		t.Errorf("config.yaml not written: %v", err)
	}
}

func TestMaybeMigrateV3_GKEProvider_Refuses(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"),
		[]byte("clusterInfrastructure:\n  provider: gke\n"), 0o644))

	err := MaybeMigrateV3(dataHome)
	if err == nil {
		t.Fatal("expected refuse-with-clear-error for gke provider")
	}
	for _, want := range []string{"gke", "values.yaml", "EducatesGKEConfig"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing hint %q", err, want)
		}
	}
	// Original file left untouched.
	if _, err := os.Stat(filepath.Join(dataHome, "values.yaml")); err != nil {
		t.Errorf("values.yaml should have been left alone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "config.yaml")); err == nil {
		t.Error("config.yaml should not have been written")
	}
}

func TestEnsureLocalConfigFile_MigratesAndPasses(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"), []byte(v3KindValues), 0o644))

	if err := EnsureLocalConfigFile(dataHome); err != nil {
		t.Fatalf("EnsureLocalConfigFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "config.yaml")); err != nil {
		t.Errorf("config.yaml missing after Ensure: %v", err)
	}
}

func TestWarnIfV3CACachePresent_OpaqueCACertOnly_Warns(t *testing.T) {
	dataHome := t.TempDir()
	secretsDir := filepath.Join(dataHome, "secrets")
	must(t, os.MkdirAll(secretsDir, 0o755))
	v3CASecret := `apiVersion: v1
kind: Secret
metadata:
  name: workshop.test-ca
  annotations:
    training.educates.dev/domain: workshop.test
type: Opaque
data:
  ca.crt: dGVzdA==
`
	must(t, os.WriteFile(filepath.Join(secretsDir, "workshop.test-ca.yaml"), []byte(v3CASecret), 0o644))

	var buf bytes.Buffer
	warnIfV3CACachePresent(&buf, secretsDir, "workshop.test")

	s := buf.String()
	for _, want := range []string{"v3-shape CA Secret detected", "kubernetes.io/tls + tls.crt + tls.key", "educates local secrets add ca workshop.test-ca --domain workshop.test"} {
		if !strings.Contains(s, want) {
			t.Errorf("warning missing %q in:\n%s", want, s)
		}
	}
}

func TestWarnIfV3CACachePresent_TLSShape_NoWarn(t *testing.T) {
	dataHome := t.TempDir()
	secretsDir := filepath.Join(dataHome, "secrets")
	must(t, os.MkdirAll(secretsDir, 0o755))
	// v4-shape — kubernetes.io/tls — should NOT warn.
	v4Secret := `apiVersion: v1
kind: Secret
metadata:
  name: workshop.test-ca
  annotations:
    training.educates.dev/domain: workshop.test
type: kubernetes.io/tls
data:
  tls.crt: dGVzdA==
  tls.key: dGVzdA==
`
	must(t, os.WriteFile(filepath.Join(secretsDir, "workshop.test-ca.yaml"), []byte(v4Secret), 0o644))

	var buf bytes.Buffer
	warnIfV3CACachePresent(&buf, secretsDir, "workshop.test")
	if buf.Len() != 0 {
		t.Errorf("v4-shape Secret should not warn, got:\n%s", buf.String())
	}
}

func TestEnsureLocalConfigFile_NoMigrationNeeded_SurfacesMissingError(t *testing.T) {
	dataHome := t.TempDir()
	err := EnsureLocalConfigFile(dataHome)
	if err == nil {
		t.Fatal("expected MissingLocalConfigError for empty data home")
	}
	if !strings.Contains(err.Error(), "config init") {
		t.Errorf("expected local config init hint, got %q", err)
	}
}

func TestEnsureOrInitLocalConfigFile_EmptyDataHome_WritesDefault(t *testing.T) {
	dataHome := filepath.Join(t.TempDir(), "missing-subdir")
	created, err := EnsureOrInitLocalConfigFile(dataHome)
	if err != nil {
		t.Fatalf("EnsureOrInitLocalConfigFile: %v", err)
	}
	if !created {
		t.Error("expected created=true when writing a fresh default config")
	}
	body := readFile(t, filepath.Join(dataHome, "config.yaml"))
	if string(body) != DefaultLocalConfigYAML {
		t.Errorf("written config does not match DefaultLocalConfigYAML:\n%s", body)
	}
}

func TestEnsureOrInitLocalConfigFile_ConfigPresent_LeavesAlone(t *testing.T) {
	dataHome := t.TempDir()
	existing := []byte("# hand-edited config\napiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n")
	must(t, os.WriteFile(filepath.Join(dataHome, "config.yaml"), existing, 0o644))

	created, err := EnsureOrInitLocalConfigFile(dataHome)
	if err != nil {
		t.Fatalf("EnsureOrInitLocalConfigFile: %v", err)
	}
	if created {
		t.Error("expected created=false when config.yaml already exists")
	}
	if got := readFile(t, filepath.Join(dataHome, "config.yaml")); string(got) != string(existing) {
		t.Errorf("existing config was overwritten:\n%s", got)
	}
}

func TestEnsureOrInitLocalConfigFile_MigratesV3_NotCreated(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"), []byte(v3KindValues), 0o644))

	created, err := EnsureOrInitLocalConfigFile(dataHome)
	if err != nil {
		t.Fatalf("EnsureOrInitLocalConfigFile: %v", err)
	}
	if created {
		t.Error("expected created=false when a v3 install was migrated")
	}
	// Migration should have produced a config carrying the v3 domain, not
	// the empty default template.
	body := readFile(t, filepath.Join(dataHome, "config.yaml"))
	if !strings.Contains(string(body), "educates.test") {
		t.Errorf("migrated config missing v3 domain:\n%s", body)
	}
}

func TestEnsureOrInitLocalConfigFile_NonKindV3_ErrorsNoDefault(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"),
		[]byte("clusterInfrastructure:\n  provider: gke\n"), 0o644))

	created, err := EnsureOrInitLocalConfigFile(dataHome)
	if err == nil {
		t.Fatal("expected migration to refuse a non-kind v3 install")
	}
	if created {
		t.Error("expected created=false when migration refuses")
	}
	if _, statErr := os.Stat(filepath.Join(dataHome, "config.yaml")); statErr == nil {
		t.Error("a default config.yaml must not be written over an unmigratable v3 install")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
