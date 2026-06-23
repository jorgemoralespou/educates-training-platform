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
	"testing"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	release "helm.sh/helm/v4/pkg/release/v1"
)

func TestFingerprint_StableAndOrderIndependent(t *testing.T) {
	a := fingerprint("1.0.0", map[string]any{"x": 1, "y": map[string]any{"b": 2, "a": 1}})
	b := fingerprint("1.0.0", map[string]any{"y": map[string]any{"a": 1, "b": 2}, "x": 1})
	if a != b {
		t.Fatalf("fingerprint not order-independent:\n %s\n %s", a, b)
	}
}

func TestFingerprint_DriftsOnValuesChange(t *testing.T) {
	a := fingerprint("1.0.0", map[string]any{"externalTrafficPolicy": "Local"})
	b := fingerprint("1.0.0", map[string]any{"externalTrafficPolicy": ""})
	if a == b {
		t.Fatal("fingerprint did not change when a value changed")
	}
}

func TestFingerprint_DriftsOnChartVersionChange(t *testing.T) {
	a := fingerprint("1.0.0", map[string]any{"x": 1})
	b := fingerprint("1.0.1", map[string]any{"x": 1})
	if a == b {
		t.Fatal("fingerprint did not change when the chart version changed")
	}
}

// int and integer-valued float must hash identically: desired values carry
// Go ints, but a release's stored Config round-trips through JSON to
// float64. A mismatch here would cause spurious drift → endless repair.
func TestFingerprint_IntFloatEquivalence(t *testing.T) {
	a := fingerprint("1.0.0", map[string]any{"replicas": int32(1)})
	b := fingerprint("1.0.0", map[string]any{"replicas": float64(1)})
	if a != b {
		t.Fatalf("int and float64 with integer value hashed differently:\n %s\n %s", a, b)
	}
}

func rel(status releasecommon.Status, chartVer string, vals map[string]any) *release.Release {
	return &release.Release{
		Info:   &release.Info{Status: status},
		Chart:  &chart.Chart{Metadata: &chart.Metadata{Version: chartVer}},
		Config: vals,
	}
}

func TestClassify(t *testing.T) {
	desired := fingerprint("1.0.0", map[string]any{"x": 1})

	tests := []struct {
		name     string
		live     *release.Release
		notFound bool
		want     decision
	}{
		{"absent", nil, true, decInstall},
		{"deployed-match", rel(releasecommon.StatusDeployed, "1.0.0", map[string]any{"x": 1}), false, decUnchanged},
		{"deployed-drift-values", rel(releasecommon.StatusDeployed, "1.0.0", map[string]any{"x": 2}), false, decUpgrade},
		{"deployed-drift-chart", rel(releasecommon.StatusDeployed, "0.9.0", map[string]any{"x": 1}), false, decUpgrade},
		{"failed-match", rel(releasecommon.StatusFailed, "1.0.0", map[string]any{"x": 1}), false, decHold},
		{"failed-drift", rel(releasecommon.StatusFailed, "1.0.0", map[string]any{"x": 2}), false, decRepair},
		{"pending-install-match", rel(releasecommon.StatusPendingInstall, "1.0.0", map[string]any{"x": 1}), false, decHold},
		{"pending-upgrade-drift", rel(releasecommon.StatusPendingUpgrade, "1.0.0", map[string]any{"x": 2}), false, decRepair},
		{"uninstalled", rel(releasecommon.StatusUninstalled, "1.0.0", map[string]any{"x": 1}), false, decInstall},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.live, tc.notFound, desired); got != tc.want {
				t.Errorf("classify = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRepairMethod(t *testing.T) {
	tests := []struct {
		name        string
		status      releasecommon.Status
		hasDeployed bool
		want        Action
	}{
		{"failed-with-deployed", releasecommon.StatusFailed, true, ActionRepairedUpgrade},
		{"failed-no-deployed", releasecommon.StatusFailed, false, ActionRepairedReinstall},
		{"pending-install-no-deployed", releasecommon.StatusPendingInstall, false, ActionRepairedReinstall},
		{"pending-upgrade-with-deployed", releasecommon.StatusPendingUpgrade, true, ActionRepairedRollback},
		{"pending-rollback-with-deployed", releasecommon.StatusPendingRollback, true, ActionRepairedRollback},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := repairMethod(tc.status, tc.hasDeployed); got != tc.want {
				t.Errorf("repairMethod(%s, %v) = %v, want %v", tc.status, tc.hasDeployed, got, tc.want)
			}
		})
	}
}
