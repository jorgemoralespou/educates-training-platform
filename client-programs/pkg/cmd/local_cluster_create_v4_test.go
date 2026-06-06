package cmd

import (
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

func TestInstallationConfigFromV4Local_CarriesKindTemplateFields(t *testing.T) {
	tr := true
	cfg := &v1alpha1.EducatesLocalConfig{
		Cluster: v1alpha1.LocalClusterConfig{
			ListenAddress: "192.168.50.50",
			ApiServer: v1alpha1.ApiServerConfig{
				Address: "192.168.50.50",
				Port:    6443,
			},
			Networking: v1alpha1.NetworkingConfig{
				ServiceSubnet: "10.96.0.0/12",
				PodSubnet:     "10.244.0.0/16",
			},
			VolumeMounts: []v1alpha1.VolumeMount{
				{HostPath: "/tmp/data", ContainerPath: "/data", ReadOnly: &tr},
			},
			RegistryMirrors: []v1alpha1.RegistryMirror{
				{Mirror: "docker.io", URL: "https://proxy.local"},
			},
		},
	}
	ic := installationConfigFromV4Local(cfg)

	if got, want := ic.LocalKindCluster.ListenAddress, "192.168.50.50"; got != want {
		t.Errorf("ListenAddress = %q, want %q", got, want)
	}
	if got, want := ic.LocalKindCluster.ApiServer.Port, 6443; got != want {
		t.Errorf("ApiServer.Port = %d, want %d", got, want)
	}
	if got, want := ic.LocalKindCluster.Networking.PodSubnet, "10.244.0.0/16"; got != want {
		t.Errorf("Networking.PodSubnet = %q, want %q", got, want)
	}
	if got := len(ic.LocalKindCluster.VolumeMounts); got != 1 {
		t.Fatalf("VolumeMounts len = %d, want 1", got)
	}
	if ic.LocalKindCluster.VolumeMounts[0].ReadOnly == nil || !*ic.LocalKindCluster.VolumeMounts[0].ReadOnly {
		t.Errorf("VolumeMounts[0].ReadOnly = %v, want true (pointer round-trip)", ic.LocalKindCluster.VolumeMounts[0].ReadOnly)
	}
	if got := len(ic.LocalKindCluster.RegistryMirrors); got != 1 {
		t.Fatalf("RegistryMirrors len = %d, want 1", got)
	}
	if got, want := ic.ClusterSecurity.PolicyEngine, "kyverno"; got != want {
		t.Errorf("ClusterSecurity.PolicyEngine = %q, want %q (laptop invariant)", got, want)
	}
}

func TestInstallationConfigFromV4Local_EmptyConfig_NoCrash(t *testing.T) {
	cfg := &v1alpha1.EducatesLocalConfig{}
	ic := installationConfigFromV4Local(cfg)
	if ic == nil {
		t.Fatal("nil result")
	}
	if got := len(ic.LocalKindCluster.VolumeMounts); got != 0 {
		t.Errorf("empty VolumeMounts len = %d, want 0", got)
	}
}
