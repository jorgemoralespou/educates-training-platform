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

// testOpts supplies a non-empty CA secret name so EducatesLocalConfig
// translation doesn't fail validation. Cluster-side secrets cache
// integration is exercised by the command-level tests.
func testOpts() Options {
	return Options{CASecretName: "test-ca", CASecretNamespace: "educates-secrets"}
}

// translateBytes is a load+translate helper for tests that build YAML
// inline (rather than referencing a testdata file). Shorter than
// writing every variant fixture to disk.
func translateBytes(t *testing.T, b []byte) (*Output, error) {
	t.Helper()
	cfg, err := config.LoadBytes(b, "inline-test")
	if err != nil {
		return nil, err
	}
	return Translate(cfg, Options{})
}

func TestTranslateLocal_EmptyConfig_AppliesInvariants(t *testing.T) {
	cfg := loadCfg(t, "local-empty.yaml")
	out, err := Translate(cfg, testOpts())
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

// TestTranslateLocal_AllLockedInvariants is the regression backstop for
// the locked Phase 5 invariants. Every row here corresponds to a single
// bullet in the "Translator invariants" section of the locked design
// (see ~/.claude/plans/reflective-dazzling-finch.md and the project
// memory). A row failing means either the translator silently dropped
// the invariant (bug; restore it) or the design changed (update the
// row + the design doc together).
//
// Adding a new locked invariant should land both a translator change
// AND a row here in the same commit.
func TestTranslateLocal_AllLockedInvariants(t *testing.T) {
	cfg := loadCfg(t, "local-empty.yaml")
	out, err := Translate(cfg, testOpts())
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	cases := []struct {
		cr   string
		path string // dotted path under <cr>.spec
		want interface{}
	}{
		// EducatesClusterConfig invariants
		{"EducatesClusterConfig", "mode", "Managed"},
		{"EducatesClusterConfig", "ingress.ingressClassName", "contour"},
		{"EducatesClusterConfig", "ingress.controller.provider", "BundledContour"},
		{"EducatesClusterConfig", "ingress.certificates.provider", "BundledCertManager"},
		{"EducatesClusterConfig", "ingress.certificates.bundledCertManager.issuerType", "CustomCA"},
		{"EducatesClusterConfig", "policyEnforcement.clusterPolicy.engine", "Kyverno"},
		{"EducatesClusterConfig", "policyEnforcement.workshopPolicy.engine", "Kyverno"},
		{"EducatesClusterConfig", "policyEnforcement.kyverno.provider", "Bundled"},

		// SessionManager invariants
		{"SessionManager", "storage.storageGroup", 1},
	}
	specs := map[string]map[string]interface{}{
		"EducatesClusterConfig": out.EducatesClusterConfig["spec"].(map[string]interface{}),
		"SessionManager":        out.SessionManager["spec"].(map[string]interface{}),
	}
	for _, tc := range cases {
		t.Run(tc.cr+":"+tc.path, func(t *testing.T) {
			got, ok := getNested(specs[tc.cr], tc.path)
			if !ok {
				t.Fatalf("invariant missing: %s.spec.%s (translator dropped it?)", tc.cr, tc.path)
			}
			if got != tc.want {
				t.Errorf("invariant value drift: %s.spec.%s = %v (%T), want %v (%T)",
					tc.cr, tc.path, got, got, tc.want, tc.want)
			}
		})
	}

	// blockedCidrs is a list — assert separately so a single missing
	// CIDR shows up cleanly.
	netRaw, ok := getNested(specs["SessionManager"], "network.blockedCidrs")
	if !ok {
		t.Fatal("invariant missing: SessionManager.spec.network.blockedCidrs")
	}
	cidrs, ok := netRaw.([]interface{})
	if !ok {
		t.Fatalf("network.blockedCidrs type = %T, want []interface{}", netRaw)
	}
	for _, want := range []string{"169.254.169.254/32", "fd00:ec2::254/128"} {
		found := false
		for _, c := range cidrs {
			if c == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("blockedCidrs missing %q (got %v)", want, cidrs)
		}
	}
}

// getNested walks a nested map[string]interface{} by dotted path.
// Returns (value, true) on hit, (nil, false) when any segment is
// missing or the intermediate node isn't a map.
func getNested(root map[string]interface{}, path string) (interface{}, bool) {
	segs := strings.Split(path, ".")
	var cur interface{} = root
	for _, s := range segs {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		v, exists := m[s]
		if !exists {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

func TestTranslateLocal_EmptyConfig_AppliesBundledKyvernoInvariant(t *testing.T) {
	cfg := loadCfg(t, "local-empty.yaml")
	out, err := Translate(cfg, testOpts())
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	spec := out.EducatesClusterConfig["spec"].(map[string]interface{})
	pe, ok := spec["policyEnforcement"].(map[string]interface{})
	if !ok {
		t.Fatalf("spec.policyEnforcement = %v, want map (BundledKyverno invariant)", spec["policyEnforcement"])
	}
	if got := pe["clusterPolicy"].(map[string]interface{})["engine"]; got != "Kyverno" {
		t.Errorf("clusterPolicy.engine = %v, want Kyverno", got)
	}
	if got := pe["workshopPolicy"].(map[string]interface{})["engine"]; got != "Kyverno" {
		t.Errorf("workshopPolicy.engine = %v, want Kyverno", got)
	}
	if got := pe["kyverno"].(map[string]interface{})["provider"]; got != "Bundled" {
		t.Errorf("kyverno.provider = %v, want Bundled", got)
	}
}

func TestTranslateLocal_LookupServiceDisabled_OmitsCR(t *testing.T) {
	cfg := loadCfg(t, "local-full.yaml") // sets lookupService: false
	out, _ := Translate(cfg, testOpts())
	if out.LookupService != nil {
		t.Errorf("LookupService = %v, want nil", out.LookupService)
	}
}

func TestTranslateLocal_FullConfig_OperatorChartValues(t *testing.T) {
	cfg := loadCfg(t, "local-full.yaml")
	out, _ := Translate(cfg, testOpts())

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
	out, _ := Translate(cfg, testOpts())
	sm := out.SessionManager["spec"].(map[string]interface{})

	if got, want := sm["defaultTheme"], "my-theme-data"; got != want {
		t.Errorf("defaultTheme = %v, want %v", got, want)
	}
	themes := sm["themes"].([]interface{})
	if len(themes) != 1 {
		t.Fatalf("themes len = %d", len(themes))
	}
	theme := themes[0].(map[string]interface{})
	source := theme["source"].(map[string]interface{})
	if got, want := source["type"], "Secret"; got != want {
		t.Errorf("source.type = %v, want %v", got, want)
	}
	secretRef := source["secretRef"].(map[string]interface{})
	if got, want := secretRef["namespace"], "educates"; got != want {
		t.Errorf("secretRef.namespace = %v, want %v", got, want)
	}
	if got, want := theme["name"], secretRef["name"]; got != want {
		t.Errorf("theme.name = %v, want backing Secret name %v", got, want)
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
	out, err := Translate(cfg, testOpts())
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
	out, _ := Translate(cfg, testOpts())

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
	out, _ := Translate(cfg, testOpts())
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
	out, _ := Translate(cfg, testOpts())
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
	out, _ := Translate(cfg, testOpts())
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
