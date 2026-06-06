package cmd

import (
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

func TestKindBootstrapFromConfig_CarriesTemplateFields(t *testing.T) {
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
		},
	}
	in := kindBootstrapFromConfig(cfg)

	if got, want := in.ListenAddress, "192.168.50.50"; got != want {
		t.Errorf("ListenAddress = %q, want %q", got, want)
	}
	if got, want := in.ApiServer.Port, 6443; got != want {
		t.Errorf("ApiServer.Port = %d, want %d", got, want)
	}
	if got, want := in.Networking.PodSubnet, "10.244.0.0/16"; got != want {
		t.Errorf("Networking.PodSubnet = %q, want %q", got, want)
	}
	if got := len(in.VolumeMounts); got != 1 {
		t.Fatalf("VolumeMounts len = %d, want 1", got)
	}
	if got, want := in.VolumeMounts[0].HostPath, "/tmp/data"; got != want {
		t.Errorf("VolumeMounts[0].HostPath = %q, want %q", got, want)
	}
}

func TestKindBootstrapFromConfig_Empty_NoCrash(t *testing.T) {
	in := kindBootstrapFromConfig(&v1alpha1.EducatesLocalConfig{})
	if in == nil {
		t.Fatal("nil result")
	}
	if got := len(in.VolumeMounts); got != 0 {
		t.Errorf("empty VolumeMounts len = %d, want 0", got)
	}
}
