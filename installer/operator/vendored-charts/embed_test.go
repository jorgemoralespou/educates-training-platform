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

package vendoredcharts

import "testing"

// TestCertManager_Embedded asserts the //go:embed directive resolved to
// a usable chart at build time. Catches the "tarball renamed but the
// embed path wasn't updated" failure at test time rather than at
// operator startup.
func TestCertManager_Embedded(t *testing.T) {
	chrt, err := CertManager()
	if err != nil {
		t.Fatalf("CertManager: %v", err)
	}
	if got, want := chrt.Metadata.Name, "cert-manager"; got != want {
		t.Errorf("chart name = %q, want %q", got, want)
	}
	if got, want := chrt.Metadata.AppVersion, CertManagerVersion; got != want {
		t.Errorf("chart appVersion = %q, want CertManagerVersion %q", got, want)
	}
}

func TestContour_Embedded(t *testing.T) {
	chrt, err := Contour()
	if err != nil {
		t.Fatalf("Contour: %v", err)
	}
	if got, want := chrt.Metadata.Name, "contour"; got != want {
		t.Errorf("chart name = %q, want %q", got, want)
	}
	if got, want := chrt.Metadata.Version, ContourChartVersion; got != want {
		t.Errorf("chart version = %q, want ContourChartVersion %q", got, want)
	}
	if got, want := chrt.Metadata.AppVersion, ContourAppVersion; got != want {
		t.Errorf("chart appVersion = %q, want ContourAppVersion %q", got, want)
	}
}

func TestExternalDNS_Embedded(t *testing.T) {
	chrt, err := ExternalDNS()
	if err != nil {
		t.Fatalf("ExternalDNS: %v", err)
	}
	if got, want := chrt.Metadata.Name, "external-dns"; got != want {
		t.Errorf("chart name = %q, want %q", got, want)
	}
	if got, want := chrt.Metadata.Version, ExternalDNSChartVersion; got != want {
		t.Errorf("chart version = %q, want ExternalDNSChartVersion %q", got, want)
	}
	if got, want := chrt.Metadata.AppVersion, ExternalDNSAppVersion; got != want {
		t.Errorf("chart appVersion = %q, want ExternalDNSAppVersion %q", got, want)
	}
}
