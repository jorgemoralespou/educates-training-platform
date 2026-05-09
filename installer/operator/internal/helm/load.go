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
	"bytes"
	"fmt"

	"helm.sh/helm/v4/pkg/chart/loader"
	chart "helm.sh/helm/v4/pkg/chart/v2"
)

// LoadArchive parses a tgz-packaged Helm chart from in-memory bytes.
// This is the canonical entry point for charts vendored under
// installer/vendored-charts/<name>-<version>.tgz: the operator embeds
// (or reads at runtime, during development) the tarball and passes the
// resulting *chart.Chart to Client.Install / Client.Upgrade.
//
// Loader.LoadArchive returns chart.Charter — an internal interface that
// has v1 and v2 implementations. The operator only deals with v2 (the
// only version Helm v4 produces), so the cast is safe; we surface a
// typed error if Helm ever hands back a different shape.
func LoadArchive(data []byte) (*chart.Chart, error) {
	c, err := loader.LoadArchive(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("load helm chart archive: %w", err)
	}
	chrt, ok := c.(*chart.Chart)
	if !ok {
		return nil, fmt.Errorf("load helm chart archive: unexpected chart type %T", c)
	}
	return chrt, nil
}
