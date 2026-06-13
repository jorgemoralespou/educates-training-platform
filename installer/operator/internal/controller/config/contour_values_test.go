/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"testing"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

func contourConfig(serviceType configv1alpha1.EnvoyServiceType) *configv1alpha1.EducatesClusterConfig {
	bc := &configv1alpha1.BundledContourConfig{}
	if serviceType != "" {
		bc.EnvoyServiceType = serviceType
	}
	return &configv1alpha1.EducatesClusterConfig{
		Spec: configv1alpha1.EducatesClusterConfigSpec{
			Ingress: &configv1alpha1.Ingress{
				Domain:           "test.example.com",
				IngressClassName: "contour",
				Controller: configv1alpha1.IngressController{
					Provider:       configv1alpha1.IngressControllerProviderBundledContour,
					BundledContour: bc,
				},
			},
		},
	}
}

func envoyUseHostPort(values map[string]any) (map[string]any, bool) {
	envoy, ok := values["envoy"].(map[string]any)
	if !ok {
		return nil, false
	}
	uhp, ok := envoy["useHostPort"].(map[string]any)
	return uhp, ok
}

// ClusterIP Envoy has no LB/NodePort, so the operator must bind the node's
// 80/443 via hostPort (the kind topology, mirroring v3).
func TestRenderContourValues_ClusterIPEnablesHostPort(t *testing.T) {
	values := renderContourValues(contourConfig(configv1alpha1.EnvoyServiceTypeClusterIP))

	uhp, ok := envoyUseHostPort(values)
	if !ok {
		t.Fatal("ClusterIP service did not enable envoy.useHostPort")
	}
	if uhp["http"] != true || uhp["https"] != true {
		t.Errorf("useHostPort = %v, want http+https true", uhp)
	}
}

// LoadBalancer (cloud) and NodePort front Envoy themselves; no hostPort.
func TestRenderContourValues_NonClusterIPNoHostPort(t *testing.T) {
	for _, st := range []configv1alpha1.EnvoyServiceType{
		configv1alpha1.EnvoyServiceTypeLoadBalancer,
		configv1alpha1.EnvoyServiceTypeNodePort,
		"", // default → LoadBalancer
	} {
		values := renderContourValues(contourConfig(st))
		if _, ok := envoyUseHostPort(values); ok {
			t.Errorf("serviceType %q unexpectedly enabled envoy.useHostPort", st)
		}
	}
}
