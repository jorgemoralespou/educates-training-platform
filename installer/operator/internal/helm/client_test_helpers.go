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
	"io"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	"helm.sh/helm/v4/pkg/registry"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	release "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
)

// memoryClientKubeVersion is the KubeVersion the in-memory test client
// reports to charts. Helm's common.DefaultCapabilities pins v1.20.0,
// which fails the kubeVersion: >=1.22 constraint cert-manager and most
// modern charts declare. We bump it to a recent supported Kubernetes
// release so tests exercise the same template logic production
// charts emit.
const memoryClientKubeVersion = "v1.31.0"

// NewMemoryClient returns a Client backed by an in-memory release store
// and Helm's no-op "printing" KubeClient. It exists to give the
// reconciler tests a Client they can drive Install/Upgrade/Uninstall/
// Status against without standing up an apiserver.
//
// This factory is exported (rather than living in _test.go) because
// reconciler-package tests in other packages use it too. It is
// nonetheless test-only — production call sites must use NewClient.
func NewMemoryClient(namespace string) (*Client, error) {
	registryClient, err := registry.NewClient()
	if err != nil {
		return nil, err
	}
	kubeVersion, err := common.ParseKubeVersion(memoryClientKubeVersion)
	if err != nil {
		return nil, err
	}
	caps := *common.DefaultCapabilities
	caps.KubeVersion = *kubeVersion
	cfg := &action.Configuration{
		Releases: storage.Init(driver.NewMemory()),
		KubeClient: &kubefake.PrintingKubeClient{
			Out: io.Discard,
		},
		Capabilities:   &caps,
		RegistryClient: registryClient,
	}
	return &Client{cfg: cfg, namespace: namespace, skipCRDs: true}, nil
}

// SeedRelease inserts a release record straight into the backing store,
// bypassing Install/Upgrade. Test-only: it lets tests stage a release in a
// specific status (notably "failed" or a "pending-*"/deployed history) before
// exercising EnsureRelease or driving a reconcile, which is otherwise hard to
// reach with the no-op fake KubeClient. Only meaningful on a memory-backed
// Client. description maps to Info.Description (surfaced by FailureMessage).
func (c *Client) SeedRelease(name string, version int, status releasecommon.Status, chrt *chart.Chart, config map[string]any, description string) error {
	return c.cfg.Releases.Create(&release.Release{
		Name:      name,
		Version:   version,
		Namespace: c.namespace,
		Info:      &release.Info{Status: status, Description: description},
		Chart:     chrt,
		Config:    config,
	})
}
