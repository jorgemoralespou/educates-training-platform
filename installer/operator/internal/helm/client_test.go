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

package helm

import (
	"context"
	"errors"
	"testing"

	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
)

// minimalChart returns a hand-built v2 chart with a single ConfigMap
// template. ConfigMap is the cheapest resource that exercises Helm's
// templating + manifest pipeline without depending on cluster-side
// validation that the fake KubeClient doesn't perform.
func minimalChart() *chart.Chart {
	return &chart.Chart{
		Metadata: &chart.Metadata{
			Name:       "test",
			Version:    "0.1.0",
			APIVersion: chart.APIVersionV2,
		},
		Templates: []*common.File{{
			Name: "templates/cm.yaml",
			Data: []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-cm
data:
  greeting: {{ .Values.greeting | default "hello" | quote }}
`),
		}},
	}
}

func TestClient_InstallStatusUninstall(t *testing.T) {
	c, err := NewMemoryClient("default")
	if err != nil {
		t.Fatalf("NewMemoryClient: %v", err)
	}

	ctx := context.Background()

	rel, err := c.Install(ctx, "demo", minimalChart(), map[string]any{"greeting": "hi"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if rel.Name != "demo" {
		t.Fatalf("Install: release name = %q, want %q", rel.Name, "demo")
	}
	if rel.Info.Status != releasecommon.StatusDeployed {
		t.Fatalf("Install: release status = %q, want %q", rel.Info.Status, releasecommon.StatusDeployed)
	}

	got, err := c.Status("demo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Name != "demo" {
		t.Fatalf("Status: release name = %q, want %q", got.Name, "demo")
	}

	if err := c.Uninstall("demo"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if _, err := c.Status("demo"); !errors.Is(err, ErrReleaseNotFound) {
		t.Fatalf("Status after Uninstall: err = %v, want ErrReleaseNotFound", err)
	}
}

func TestClient_StatusOnMissingRelease(t *testing.T) {
	c, err := NewMemoryClient("default")
	if err != nil {
		t.Fatalf("NewMemoryClient: %v", err)
	}

	if _, err := c.Status("does-not-exist"); !errors.Is(err, ErrReleaseNotFound) {
		t.Fatalf("Status: err = %v, want ErrReleaseNotFound", err)
	}
}

func TestClient_UninstallIsIdempotent(t *testing.T) {
	c, err := NewMemoryClient("default")
	if err != nil {
		t.Fatalf("NewMemoryClient: %v", err)
	}

	// Uninstalling a release that never existed should be a no-op,
	// matching the contract documented on Client.Uninstall.
	if err := c.Uninstall("never-installed"); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
}
