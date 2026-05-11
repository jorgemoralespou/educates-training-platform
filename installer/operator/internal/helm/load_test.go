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
	"os"
	"path/filepath"
	"testing"
)

// TestLoadArchive_VendoredCertManager exercises the full vendor → load
// pipeline against the real cert-manager tarball checked into
// installer/operator/vendored-charts/. If this test fails after a chart
// bump, the bump procedure in vendored-charts/README.md was likely
// skipped.
func TestLoadArchive_VendoredCertManager(t *testing.T) {
	path := filepath.Join("..", "..", "vendored-charts", "cert-manager-v1.20.2.tgz")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vendored chart: %v", err)
	}

	chrt, err := LoadArchive(data)
	if err != nil {
		t.Fatalf("LoadArchive: %v", err)
	}

	if got, want := chrt.Metadata.Name, "cert-manager"; got != want {
		t.Errorf("chart name = %q, want %q", got, want)
	}
	if got, want := chrt.Metadata.AppVersion, "v1.20.2"; got != want {
		t.Errorf("chart appVersion = %q, want %q", got, want)
	}
}
