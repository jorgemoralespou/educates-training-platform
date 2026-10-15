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

package platform

import (
	"testing"

	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
)

// TestResolveNodeCATrust covers the node CA injector's tri-state against
// the CA the session manager uses: the cluster's ingress CA, or the
// SessionManager's CA override, which replaces it.
func TestResolveNodeCATrust(t *testing.T) {
	withClusterCA := valuesTestClusterConfig()
	withoutClusterCA := valuesTestClusterConfig()
	withoutClusterCA.Status.Ingress.CACertificateSecretRef = nil

	sessionManager := func(mode platformv1alpha1.ComponentMode, override bool) *platformv1alpha1.SessionManager {
		obj := &platformv1alpha1.SessionManager{}
		if mode != "" {
			obj.Spec.NodeCATrust = &platformv1alpha1.NodeCATrust{Mode: mode}
		}
		if override {
			obj.Spec.IngressOverrides = &platformv1alpha1.IngressOverrides{
				CACertificateSecretRef: &platformv1alpha1.SecretReference{Name: "override-ca"},
			}
		}
		return obj
	}

	tests := []struct {
		name       string
		obj        *platformv1alpha1.SessionManager
		clusterCA  bool
		wantIntent extrasIntent
		wantReason string
	}{
		{"Auto without any CA skips", sessionManager("", false), false, intentSkip, "ModeAutoNoCA"},
		{"Auto with the cluster CA installs", sessionManager("", false), true, intentInstall, reasonExtraInstalled},
		{"Auto with only a CA override installs", sessionManager("", true), false, intentInstall, reasonExtraInstalled},
		{"Enabled with only a CA override installs", sessionManager(platformv1alpha1.ComponentModeEnabled, true), false, intentInstall, reasonExtraInstalled},
		{"Enabled without any CA refuses", sessionManager(platformv1alpha1.ComponentModeEnabled, false), false, intentRefuse, "NodeCATrustMissingCA"},
		{"Disabled with a CA override skips", sessionManager(platformv1alpha1.ComponentModeDisabled, true), true, intentSkip, "ModeDisabled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := withoutClusterCA
			if tt.clusterCA {
				cfg = withClusterCA
			}
			intent, reason, _ := resolveNodeCATrust(tt.obj, cfg)
			if intent != tt.wantIntent || reason != tt.wantReason {
				t.Errorf("resolveNodeCATrust() = (%v, %q), want (%v, %q)", intent, reason, tt.wantIntent, tt.wantReason)
			}
		})
	}
}
