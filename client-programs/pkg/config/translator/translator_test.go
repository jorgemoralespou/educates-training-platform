package translator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

func loadCfg(t *testing.T, fixture string) v1alpha1.Config {
	t.Helper()
	cfg, err := config.Load(filepath.Join("..", "testdata", fixture))
	if err != nil {
		t.Fatalf("Load %s: %v", fixture, err)
	}
	return cfg
}

func TestTranslateLocal_EmptyConfig_AppliesInvariants(t *testing.T) {
	cfg := loadCfg(t, "local-empty.yaml")
	out, err := Translate(cfg)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	ecc := out.EducatesClusterConfig
	if got, want := ecc["apiVersion"], "config.educates.dev/v1alpha1"; got != want {
		t.Errorf("ECC apiVersion = %v, want %v", got, want)
	}
	if got, want := ecc["kind"], "EducatesClusterConfig"; got != want {
		t.Errorf("ECC kind = %v, want %v", got, want)
	}

	spec := ecc["spec"].(map[string]interface{})
	if got, want := spec["mode"], "Managed"; got != want {
		t.Errorf("spec.mode = %v, want %v", got, want)
	}

	ingress := spec["ingress"].(map[string]interface{})
	if got, want := ingress["ingressClassName"], "contour"; got != want {
		t.Errorf("ingress.ingressClassName = %v, want %v", got, want)
	}
	if _, set := ingress["domain"]; set {
		t.Errorf("ingress.domain set unexpectedly: %v (host-IP defaulting belongs upstream)", ingress["domain"])
	}

	certs := ingress["certificates"].(map[string]interface{})
	if got, want := certs["provider"], "BundledCertManager"; got != want {
		t.Errorf("certificates.provider = %v, want %v", got, want)
	}
	cm := certs["bundledCertManager"].(map[string]interface{})
	if got, want := cm["issuerType"], "CustomCA"; got != want {
		t.Errorf("issuerType = %v, want %v", got, want)
	}

	// Defaults from WithDefaults flow through: LookupService=true → present;
	// imagePrePuller=false → SessionManager has imagePrePuller.enabled: false;
	// logLevel=info → on every spec.
	if out.LookupService == nil {
		t.Error("LookupService = nil, want present (default lookupService=true)")
	}
	sm := out.SessionManager["spec"].(map[string]interface{})
	if got, want := sm["logLevel"], "info"; got != want {
		t.Errorf("SessionManager logLevel = %v, want %v", got, want)
	}
	ipp := sm["imagePrePuller"].(map[string]interface{})
	if got, want := ipp["enabled"], false; got != want {
		t.Errorf("imagePrePuller.enabled = %v, want %v", got, want)
	}
}

func TestTranslateLocal_LookupServiceDisabled_OmitsCR(t *testing.T) {
	cfg := loadCfg(t, "local-full.yaml") // sets lookupService: false
	out, _ := Translate(cfg)
	if out.LookupService != nil {
		t.Errorf("LookupService = %v, want nil", out.LookupService)
	}
}

func TestTranslateLocal_FullConfig_OperatorChartValues(t *testing.T) {
	cfg := loadCfg(t, "local-full.yaml")
	out, _ := Translate(cfg)

	values := out.OperatorChartValues
	image := values["image"].(map[string]interface{})
	if got, want := image["repository"], "ghcr.io/educates/educates-operator"; got != want {
		t.Errorf("image.repository = %v, want %v", got, want)
	}
	if got, want := image["tag"], "4.0.0"; got != want {
		t.Errorf("image.tag = %v, want %v", got, want)
	}

	secrets := values["imagePullSecrets"].([]interface{})
	if len(secrets) != 1 {
		t.Fatalf("imagePullSecrets len = %d, want 1", len(secrets))
	}
	if got, want := secrets[0].(map[string]interface{})["name"], "operator-pull-secret"; got != want {
		t.Errorf("imagePullSecrets[0].name = %v, want %v (k8s [{name:}] shape)", got, want)
	}

	if got, want := values["logLevel"], "debug"; got != want {
		t.Errorf("operator logLevel = %v, want %v", got, want)
	}
}

func TestTranslateLocal_FullConfig_SessionManagerFields(t *testing.T) {
	cfg := loadCfg(t, "local-full.yaml")
	out, _ := Translate(cfg)
	sm := out.SessionManager["spec"].(map[string]interface{})

	if got, want := sm["defaultTheme"], "educates-default"; got != want {
		t.Errorf("defaultTheme = %v, want %v", got, want)
	}
	themes := sm["themes"].(map[string]interface{})
	refs := themes["dataRefs"].([]interface{})
	if len(refs) != 1 {
		t.Fatalf("dataRefs len = %d", len(refs))
	}
	ref := refs[0].(map[string]interface{})
	if got, want := ref["namespace"], "educates"; got != want {
		t.Errorf("ref.namespace = %v, want %v", got, want)
	}

	ipp := sm["imagePrePuller"].(map[string]interface{})
	if got, want := ipp["enabled"], true; got != want {
		t.Errorf("imagePrePuller.enabled = %v, want true", got)
	}

	images := sm["images"].(map[string]interface{})
	overrides := images["overrides"].([]interface{})
	if len(overrides) != 1 {
		t.Fatalf("overrides len = %d", len(overrides))
	}
}

func TestTranslateEscape_Minimal_Passthrough(t *testing.T) {
	cfg := loadCfg(t, "escape-minimal.yaml")
	out, err := Translate(cfg)
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	// All 4 CRs present, all specs empty (no fields declared by user).
	for _, kind := range []struct {
		name string
		got  map[string]interface{}
	}{
		{"ECC", out.EducatesClusterConfig},
		{"SecretsManager", out.SecretsManager},
		{"SessionManager", out.SessionManager},
	} {
		if kind.got == nil {
			t.Errorf("%s: nil, want present", kind.name)
		}
	}
	// LookupService omitted (cfg.LookupService is nil).
	if out.LookupService != nil {
		t.Errorf("LookupService = %v, want nil", out.LookupService)
	}
	// No CLI-side defaults: operator chart values are empty.
	if len(out.OperatorChartValues) != 0 {
		t.Errorf("OperatorChartValues = %v, want empty (no defaulting on escape kind)", out.OperatorChartValues)
	}
}

func TestTranslateEscape_WithTarget_PassesAllSections(t *testing.T) {
	cfg := loadCfg(t, "escape-with-target.yaml")
	out, _ := Translate(cfg)

	if got, want := out.OperatorChartValues["logLevel"], "debug"; got != want {
		t.Errorf("operator logLevel = %v, want %v", got, want)
	}
	// SecretsManager spec is {} from the fixture; still wrapped.
	if out.SecretsManager["spec"] == nil {
		t.Errorf("SecretsManager.spec = nil, want {}")
	}
}

func TestRender_CRs_MultiDocYAML(t *testing.T) {
	cfg := loadCfg(t, "local-empty.yaml")
	out, _ := Translate(cfg)
	yamlBytes, err := RenderCRs(out)
	if err != nil {
		t.Fatalf("RenderCRs: %v", err)
	}

	s := string(yamlBytes)
	// Multi-doc: 3 docs (ECC + SecretsManager + SessionManager since
	// LookupService default is true → 4 docs). Each doc starts at the
	// beginning of a line. Count occurrences of "kind:" at line start
	// — yaml.v3 emits one per top-level map.
	if got := strings.Count(s, "\nkind:") + strings.Count(s, "kind:"); got < 4 {
		t.Errorf("expected at least 4 'kind:' lines (4 CRs), got %d in:\n%s", got, s)
	}
	for _, kind := range []string{"EducatesClusterConfig", "SecretsManager", "LookupService", "SessionManager"} {
		if !strings.Contains(s, "kind: "+kind) {
			t.Errorf("output missing kind %s:\n%s", kind, s)
		}
	}
	// metadata.name: cluster on each CR.
	if got := strings.Count(s, "name: cluster"); got < 4 {
		t.Errorf("expected at least 4 'name: cluster' lines, got %d", got)
	}
}

func TestRender_OperatorValues_Empty(t *testing.T) {
	cfg := loadCfg(t, "local-empty.yaml")
	out, _ := Translate(cfg)
	values, err := RenderOperatorValues(out)
	if err != nil {
		t.Fatalf("RenderOperatorValues: %v", err)
	}
	// Empty local config has no operator overrides → just "{}\n".
	// (logLevel comes from WithDefaults, so actually it will have logLevel: info.)
	s := string(values)
	if !strings.Contains(s, "logLevel: info") {
		t.Errorf("expected logLevel: info in values, got:\n%s", s)
	}
}

func TestRender_OperatorValues_Full(t *testing.T) {
	cfg := loadCfg(t, "local-full.yaml")
	out, _ := Translate(cfg)
	values, _ := RenderOperatorValues(out)
	s := string(values)
	for _, want := range []string{
		"repository: ghcr.io/educates/educates-operator",
		"tag: 4.0.0",
		"logLevel: debug",
		"name: operator-pull-secret",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("values missing %q:\n%s", want, s)
		}
	}
}
