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

// Package vendoredcharts embeds the upstream Helm chart tarballs the
// operator drives via the Helm SDK. Tarballs are colocated with this
// Go file (//go:embed cannot reach outside the package directory) and
// kept integrity-pinned via SHA256SUMS in the same directory; refresh
// them with `make vendor-charts` from the operator root.
//
// Each chart has a typed accessor returning a parsed *chart.Chart
// ready to hand to internal/helm.Client. Versions are exported as
// constants so reconcilers can publish them in
// status.bundledChartVersions without re-parsing the chart bytes.
package vendoredcharts

import (
	_ "embed"

	chart "helm.sh/helm/v4/pkg/chart/v2"

	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
)

// CertManagerVersion mirrors the embedded tarball's appVersion and the
// vendored-charts/SHA256SUMS entry; bumped only when the tarball is
// replaced via `make vendor-charts`.
const CertManagerVersion = "v1.20.2"

//go:embed cert-manager-v1.20.2.tgz
var certManagerTarball []byte

// CertManager parses the embedded cert-manager tarball and returns a
// chart ready for the Helm SDK. Each call re-parses the bytes; the
// caller is responsible for caching if the parse cost matters.
func CertManager() (*chart.Chart, error) {
	return helm.LoadArchive(certManagerTarball)
}

// ContourChartVersion is the upstream Helm chart version (semver
// version of the *chart*, distinct from the Contour appVersion).
// Surfaced in status.bundledChartVersions["contour"].
const ContourChartVersion = "0.5.0"

// ContourAppVersion is the Project Contour binary version the
// embedded chart installs. Less load-bearing than ContourChartVersion
// (the chart version is what's pinned in our build), but useful to
// expose in logs/diagnostics so a reader doesn't have to crack the
// tarball to learn which Contour they're running.
const ContourAppVersion = "1.33.4"

//go:embed contour-0.5.0.tgz
var contourTarball []byte

// Contour parses the embedded Project Contour chart tarball and
// returns a chart ready for the Helm SDK. Source:
// https://github.com/projectcontour/helm-charts/releases.
func Contour() (*chart.Chart, error) {
	return helm.LoadArchive(contourTarball)
}

// ExternalDNSChartVersion is the upstream Helm chart version
// (semver of the *chart*, distinct from the external-dns
// appVersion). Surfaced in
// status.bundledChartVersions["external-dns"].
const ExternalDNSChartVersion = "1.21.1"

// ExternalDNSAppVersion is the kubernetes-sigs/external-dns binary
// version the embedded chart installs.
const ExternalDNSAppVersion = "0.21.0"

//go:embed external-dns-1.21.1.tgz
var externalDNSTarball []byte

// ExternalDNS parses the embedded kubernetes-sigs external-dns
// chart tarball and returns a chart ready for the Helm SDK.
// Source: https://github.com/kubernetes-sigs/external-dns
// (helm-chart-1.21.1 release).
func ExternalDNS() (*chart.Chart, error) {
	return helm.LoadArchive(externalDNSTarball)
}
