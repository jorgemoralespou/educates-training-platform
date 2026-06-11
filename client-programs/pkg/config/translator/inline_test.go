package translator

import (
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

func TestTranslateInline_Minimal_ModeInline(t *testing.T) {
	cfg := loadCfg(t, "inline-minimal.yaml").(*v1alpha1.EducatesInlineConfig)
	out, err := Translate(cfg, Options{}) // Inline ignores CASecret*; no opts needed
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	spec := out.EducatesClusterConfig["spec"].(map[string]interface{})
	if got, want := spec["mode"], "Inline"; got != want {
		t.Errorf("spec.mode = %v, want %v", got, want)
	}
	// CEL forbids the Managed-mode top-level fields under Inline; ensure
	// the translator doesn't accidentally emit any.
	for _, forbidden := range []string{"ingress", "dns", "policyEnforcement", "imageRegistry", "infrastructure"} {
		if _, set := spec[forbidden]; set {
			t.Errorf("spec.%s set in Inline mode (forbidden by CRD CEL)", forbidden)
		}
	}

	inline := spec["inline"].(map[string]interface{})
	ingress := inline["ingress"].(map[string]interface{})
	if got, want := ingress["domain"], "workshop.test"; got != want {
		t.Errorf("inline.ingress.domain = %v, want %v", got, want)
	}
	if got, want := ingress["ingressClassName"], "contour"; got != want {
		t.Errorf("inline.ingress.ingressClassName = %v, want %v", got, want)
	}
	wildcardRef := ingress["wildcardCertificateSecretRef"].(map[string]interface{})
	if got, want := wildcardRef["name"], "educates-wildcard-tls"; got != want {
		t.Errorf("wildcardCertificateSecretRef.name = %v, want %v", got, want)
	}
	if _, set := ingress["caCertificateSecretRef"]; set {
		t.Errorf("caCertificateSecretRef set when not provided in config")
	}

	// Default policy is Kyverno (matches CRD kubebuilder default).
	pe := inline["policyEnforcement"].(map[string]interface{})
	if got, want := pe["clusterPolicyEngine"], "Kyverno"; got != want {
		t.Errorf("clusterPolicyEngine default = %v, want %v", got, want)
	}
	if got, want := pe["workshopPolicyEngine"], "Kyverno"; got != want {
		t.Errorf("workshopPolicyEngine default = %v, want %v", got, want)
	}
}

func TestTranslateInline_OpenShift_FullFieldPassthrough(t *testing.T) {
	cfg := loadCfg(t, "inline-openshift.yaml").(*v1alpha1.EducatesInlineConfig)
	out, err := Translate(cfg, Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	inline := out.EducatesClusterConfig["spec"].(map[string]interface{})["inline"].(map[string]interface{})
	ingress := inline["ingress"].(map[string]interface{})
	if got, want := ingress["caCertificateSecretRef"].(map[string]interface{})["name"], "educates-wildcard-ca"; got != want {
		t.Errorf("caCertificateSecretRef.name = %v, want %v", got, want)
	}

	pe := inline["policyEnforcement"].(map[string]interface{})
	if got, want := pe["clusterPolicyEngine"], "OpenShiftSCC"; got != want {
		t.Errorf("clusterPolicyEngine = %v, want %v", got, want)
	}
	if got, want := pe["workshopPolicyEngine"], "None"; got != want {
		t.Errorf("workshopPolicyEngine = %v, want %v", got, want)
	}

	ir := inline["imageRegistry"].(map[string]interface{})
	if got, want := ir["prefix"], "registry.internal.example.com/educates"; got != want {
		t.Errorf("imageRegistry.prefix = %v, want %v", got, want)
	}
	pullSecrets := ir["pullSecrets"].([]interface{})
	if len(pullSecrets) != 1 {
		t.Fatalf("pullSecrets len = %d, want 1", len(pullSecrets))
	}
	if got, want := pullSecrets[0].(map[string]interface{})["name"], "internal-registry-pull"; got != want {
		t.Errorf("pullSecrets[0].name = %v, want %v (k8s {name:} shape)", got, want)
	}
}

func TestTranslateInline_RenderRoundTripsAsValidYAML(t *testing.T) {
	cfg := loadCfg(t, "inline-openshift.yaml").(*v1alpha1.EducatesInlineConfig)
	out, _ := Translate(cfg, Options{})
	crs, err := RenderCRs(out)
	if err != nil {
		t.Fatalf("RenderCRs: %v", err)
	}
	s := string(crs)
	for _, want := range []string{
		"mode: Inline",
		"domain: workshops.example.com",
		"clusterPolicyEngine: OpenShiftSCC",
		"kind: EducatesClusterConfig",
		"kind: SecretsManager",
		"kind: SessionManager",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered output missing %q", want)
		}
	}
}

// Inline-mode BYO clusters behind a corporate load balancer use the
// same externalTLSTermination assertion as the cloud kinds.
func TestTranslateInline_ExternalTLSTermination_SetsSessionManagerProtocol(t *testing.T) {
	out, err := translateBytes(t, []byte(`
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesInlineConfig
domain: workshops.example.com
ingressClassName: contour
wildcardCertificateSecret: wildcard-tls
externalTLSTermination: true
`))
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	spec := out.SessionManager["spec"].(map[string]interface{})
	overrides, ok := spec["ingressOverrides"].(map[string]interface{})
	if !ok {
		t.Fatalf("sessionManager spec.ingressOverrides missing: %v", spec)
	}
	if got, want := overrides["protocol"], "https"; got != want {
		t.Errorf("ingressOverrides.protocol = %v, want %v", got, want)
	}
}
