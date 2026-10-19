package cmd

import (
	"reflect"
	"testing"
)

func TestLoadResolverInputs_EscapeConfig_TakesItsIngressDomain(t *testing.T) {
	path := writeFixture(t, `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesConfig
target:
  provider: kind
  resolver:
    targetAddress: 192.168.50.50
    extraDomains:
    - extra.example.test
educatesClusterConfig:
  mode: Managed
  ingress:
    domain: workshops.example.test
    ingressClassName: contour
    controller:
      provider: BundledContour
    certificates:
      provider: None
`)
	cfg, err := loadResolverInputs(path)
	if err != nil {
		t.Fatalf("loadResolverInputs: %v", err)
	}
	if got, want := cfg.Ingress.Domain, "workshops.example.test"; got != want {
		t.Errorf("Ingress.Domain = %q, want %q", got, want)
	}
	if got, want := cfg.Resolver.TargetAddress, "192.168.50.50"; got != want {
		t.Errorf("Resolver.TargetAddress = %q, want %q", got, want)
	}
	if got, want := cfg.Resolver.ExtraDomains, []string{"extra.example.test"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Resolver.ExtraDomains = %v, want %v", got, want)
	}
}
