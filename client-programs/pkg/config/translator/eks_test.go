package translator

import (
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

func TestTranslateEKS_Minimal_InvariantsAndIRSARoleDefaults(t *testing.T) {
	cfg := loadCfg(t, "eks-minimal.yaml").(*v1alpha1.EducatesEKSConfig)
	out, err := Translate(cfg, Options{})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}

	spec := out.EducatesClusterConfig["spec"].(map[string]interface{})
	if got, want := spec["mode"], "Managed"; got != want {
		t.Errorf("spec.mode = %v, want %v", got, want)
	}

	dns01 := spec["ingress"].(map[string]interface{})["certificates"].(map[string]interface{})["bundledCertManager"].(map[string]interface{})["acme"].(map[string]interface{})["solvers"].(map[string]interface{})["dns01"].(map[string]interface{})
	if got, want := dns01["provider"], "Route53"; got != want {
		t.Errorf("dns01.provider = %v, want %v", got, want)
	}
	route53 := dns01["route53"].(map[string]interface{})
	if got, want := route53["hostedZoneID"], "Z0123456789ABCDEF"; got != want {
		t.Errorf("route53.hostedZoneID = %v, want %v", got, want)
	}
	if got, want := route53["region"], "us-east-1"; got != want {
		t.Errorf("route53.region = %v, want %v", got, want)
	}
	if got, want := route53["iamRoleARN"], "arn:aws:iam::123456789012:role/educates-cert-manager"; got != want {
		t.Errorf("cert-manager iamRoleARN default = %v, want %v", got, want)
	}

	bundledDNS := spec["dns"].(map[string]interface{})["bundledExternalDNS"].(map[string]interface{})
	externalRoute53 := bundledDNS["route53"].(map[string]interface{})
	if got, want := externalRoute53["iamRoleARN"], "arn:aws:iam::123456789012:role/educates-external-dns"; got != want {
		t.Errorf("external-dns iamRoleARN default = %v, want %v", got, want)
	}

	if got, want := spec["policyEnforcement"].(map[string]interface{})["clusterPolicy"].(map[string]interface{})["engine"], "Kyverno"; got != want {
		t.Errorf("clusterPolicy.engine = %v, want %v", got, want)
	}
}

func TestTranslateEKS_OverrideRoles_RoundTrips(t *testing.T) {
	yaml := `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesEKSConfig
aws:
  accountId: "123456789012"
  region: us-east-1
  route53HostedZoneId: Z0123456789ABCDEF
  certManagerRoleARN: arn:aws:iam::123456789012:role/custom-cm
  externalDNSRoleARN: arn:aws:iam::123456789012:role/custom-edns
domain: academy-01.workshops.example.com
acme:
  email: ops@example.com
`
	out, err := translateBytes(t, []byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	spec := out.EducatesClusterConfig["spec"].(map[string]interface{})
	r53 := spec["ingress"].(map[string]interface{})["certificates"].(map[string]interface{})["bundledCertManager"].(map[string]interface{})["acme"].(map[string]interface{})["solvers"].(map[string]interface{})["dns01"].(map[string]interface{})["route53"].(map[string]interface{})
	if got, want := r53["iamRoleARN"], "arn:aws:iam::123456789012:role/custom-cm"; got != want {
		t.Errorf("user-provided role = %v, want %v (defaulting should NOT override)", got, want)
	}
}

func TestTranslateEKS_RenderRoundTripsAsValidYAML(t *testing.T) {
	cfg := loadCfg(t, "eks-minimal.yaml").(*v1alpha1.EducatesEKSConfig)
	out, _ := Translate(cfg, Options{})
	crs, err := RenderCRs(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(crs)
	for _, want := range []string{
		"mode: Managed",
		"provider: Route53",
		"hostedZoneID: Z0123456789ABCDEF",
		"iamRoleARN: arn:aws:iam::123456789012:role/educates-cert-manager",
		"iamRoleARN: arn:aws:iam::123456789012:role/educates-external-dns",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered output missing %q", want)
		}
	}
}
