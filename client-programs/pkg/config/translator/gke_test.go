package translator

import (
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

func TestTranslateGKE_Minimal_InvariantsAndProjectDefaults(t *testing.T) {
	cfg := loadCfg(t, "gke-minimal.yaml").(*v1alpha1.EducatesGKEConfig)
	out, err := Translate(cfg, Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	spec := out.EducatesClusterConfig["spec"].(map[string]interface{})
	if got, want := spec["mode"], "Managed"; got != want {
		t.Errorf("spec.mode = %v, want %v", got, want)
	}

	ingress := spec["ingress"].(map[string]interface{})
	if got, want := ingress["domain"], "academy-01.google.educates.dev"; got != want {
		t.Errorf("ingress.domain = %v, want %v", got, want)
	}
	controller := ingress["controller"].(map[string]interface{})
	bundled := controller["bundledContour"].(map[string]interface{})
	if got, want := bundled["envoyServiceType"], "LoadBalancer"; got != want {
		t.Errorf("envoyServiceType = %v, want %v (cloud invariant)", got, want)
	}

	bcm := ingress["certificates"].(map[string]interface{})["bundledCertManager"].(map[string]interface{})
	if got, want := bcm["issuerType"], "ACME"; got != want {
		t.Errorf("issuerType = %v, want %v", got, want)
	}
	acme := bcm["acme"].(map[string]interface{})
	if got, want := acme["email"], "ops@example.com"; got != want {
		t.Errorf("acme.email = %v, want %v", got, want)
	}
	dns01 := acme["solvers"].(map[string]interface{})["dns01"].(map[string]interface{})
	if got, want := dns01["provider"], "CloudDNS"; got != want {
		t.Errorf("solvers.dns01.provider = %v, want %v", got, want)
	}
	cloudDNS := dns01["cloudDNS"].(map[string]interface{})
	if got, want := cloudDNS["project"], "my-gcp-project"; got != want {
		t.Errorf("cloudDNS.project = %v, want %v", got, want)
	}
	// WI service-account default derives from project.
	if got, want := cloudDNS["workloadIdentityServiceAccount"], "cert-manager@my-gcp-project.iam.gserviceaccount.com"; got != want {
		t.Errorf("certmanager WI SA default = %v, want %v", got, want)
	}

	dns := spec["dns"].(map[string]interface{})
	bundledDNS := dns["bundledExternalDNS"].(map[string]interface{})
	externalDNSCloudDNS := bundledDNS["cloudDNS"].(map[string]interface{})
	if got, want := externalDNSCloudDNS["workloadIdentityServiceAccount"], "external-dns@my-gcp-project.iam.gserviceaccount.com"; got != want {
		t.Errorf("external-dns WI SA default = %v, want %v", got, want)
	}

	// Kyverno invariant present.
	pe := spec["policyEnforcement"].(map[string]interface{})
	if got := pe["clusterPolicy"].(map[string]interface{})["engine"]; got != "Kyverno" {
		t.Errorf("clusterPolicy.engine = %v, want Kyverno", got)
	}
}

func TestTranslateGKE_OverrideServiceAccounts_RoundTrips(t *testing.T) {
	yaml := `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesGKEConfig
gcp:
  project: my-gcp-project
  certManagerServiceAccount: custom-cm@my-gcp-project.iam.gserviceaccount.com
  externalDNSServiceAccount: custom-edns@my-gcp-project.iam.gserviceaccount.com
domain: academy-01.google.educates.dev
acme:
  email: ops@example.com
`
	out, err := translateBytes(t, []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	spec := out.EducatesClusterConfig["spec"].(map[string]interface{})
	cloudDNS := spec["ingress"].(map[string]interface{})["certificates"].(map[string]interface{})["bundledCertManager"].(map[string]interface{})["acme"].(map[string]interface{})["solvers"].(map[string]interface{})["dns01"].(map[string]interface{})["cloudDNS"].(map[string]interface{})
	if got, want := cloudDNS["workloadIdentityServiceAccount"], "custom-cm@my-gcp-project.iam.gserviceaccount.com"; got != want {
		t.Errorf("user-provided WI SA = %v, want %v (defaulting should NOT override)", got, want)
	}
}

func TestTranslateGKE_RenderRoundTripsAsValidYAML(t *testing.T) {
	cfg := loadCfg(t, "gke-minimal.yaml").(*v1alpha1.EducatesGKEConfig)
	out, _ := Translate(cfg, Options{})
	crs, err := RenderCRs(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(crs)
	for _, want := range []string{
		"mode: Managed",
		"envoyServiceType: LoadBalancer",
		"issuerType: ACME",
		"provider: CloudDNS",
		"workloadIdentityServiceAccount: cert-manager@my-gcp-project",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered output missing %q", want)
		}
	}
}

// externalTLSTermination asserts the public edge is https when TLS is
// terminated at an external load balancer — it must surface as the
// SessionManager ingressOverrides protocol, and stay absent otherwise.
func TestTranslateGKE_ExternalTLSTermination_SetsSessionManagerProtocol(t *testing.T) {
	out, err := translateBytes(t, []byte(`
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesGKEConfig
gcp:
  project: my-gcp-project
domain: academy-01.google.educates.dev
acme:
  email: ops@example.com
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

	// Default (field unset) must not emit the override.
	cfg := loadCfg(t, "gke-minimal.yaml").(*v1alpha1.EducatesGKEConfig)
	out, err = Translate(cfg, Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	spec = out.SessionManager["spec"].(map[string]interface{})
	if _, present := spec["ingressOverrides"]; present {
		t.Errorf("ingressOverrides unexpectedly present without externalTLSTermination: %v", spec)
	}
}
