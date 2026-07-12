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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	release "helm.sh/helm/v4/pkg/release/v1"
)

// Action reports what EnsureRelease actually did, so callers can set
// conditions and decide whether to proceed to readiness checks.
type Action string

const (
	// ActionInstalled: no release existed; a fresh install ran.
	ActionInstalled Action = "installed"
	// ActionUpgraded: a deployed release drifted from desired; upgraded.
	ActionUpgraded Action = "upgraded"
	// ActionRepairedUpgrade: a failed release with a prior deployed
	// revision was upgraded in place (non-destructive).
	ActionRepairedUpgrade Action = "repaired-upgrade"
	// ActionRepairedRollback: a pending (lock-stuck) release with a prior
	// deployed revision was rolled back to that revision. The release is
	// now deployed at the last good config; the next reconcile observes
	// fingerprint drift and upgrades to desired.
	ActionRepairedRollback Action = "repaired-rollback"
	// ActionRepairedReinstall: a failed/pending release with no deployed
	// revision (a failed first install) was uninstalled and reinstalled —
	// the only recovery Helm allows when there is nothing to upgrade from.
	ActionRepairedReinstall Action = "repaired-reinstall"
	// ActionUnchanged: a deployed release already matches desired.
	ActionUnchanged Action = "unchanged"
	// ActionHeldFailed: a failed/pending release whose desired fingerprint
	// is unchanged since it was last attempted. Re-running would fail the
	// same way, so EnsureRelease does nothing and the caller surfaces the
	// failure. This is the brake that prevents reinstall churn.
	ActionHeldFailed Action = "held-failed"
	// ActionWaitingUninstall: the release is transiently mid-uninstall
	// (StatusUninstalling) because a reconcile raced an in-flight teardown.
	// EnsureRelease neither installs (Helm rejects re-using a name that is
	// still in use) nor errors; the caller requeues until the uninstall
	// settles, keeping the teardown race out of the ERROR log.
	ActionWaitingUninstall Action = "waiting-uninstall"
)

// Result is the outcome of EnsureRelease. Release is the live or resulting
// release record; it is nil only when an Install errored before a record
// could be written.
type Result struct {
	Action  Action
	Release *release.Release
}

// EnsureRelease converges the named release toward (chrt, vals) and reports
// what it did. It replaces the per-reconciler "Status → Install on NotFound
// / Upgrade on chart-version drift" routing with a single policy that also:
//
//   - upgrades on *values* drift, not just chart-version drift (so a fix
//     shipped in a new operator image — which changes rendered values but
//     not the chart version — is picked up);
//   - never silently treats a failed/pending release as healthy; and
//   - self-heals a failed release once its desired fingerprint changes,
//     choosing the least-destructive recovery (see repairMethod), while
//     holding (not churning) a failed release whose inputs are unchanged.
//
// Readiness of the installed workloads remains the caller's concern — this
// only governs the Helm release lifecycle.
func (c *Client) EnsureRelease(ctx context.Context, name string, chrt *chart.Chart, vals map[string]any) (Result, error) {
	desiredFP := fingerprint(chartVersion(chrt), vals)

	live, err := c.Status(name)
	notFound := errors.Is(err, ErrReleaseNotFound)
	if err != nil && !notFound {
		return Result{}, err
	}

	switch classify(live, notFound, desiredFP) {
	case decInstall:
		r, err := c.Install(ctx, name, chrt, vals)
		return Result{Action: ActionInstalled, Release: r}, err

	case decUpgrade:
		r, err := c.Upgrade(ctx, name, chrt, vals)
		return Result{Action: ActionUpgraded, Release: r}, err

	case decRepair:
		rev, hasDeployed, err := c.LastDeployedRevision(name)
		if err != nil {
			return Result{}, err
		}
		switch repairMethod(live.Info.Status, hasDeployed) {
		case ActionRepairedRollback:
			if err := c.Rollback(name, rev); err != nil {
				return Result{}, err
			}
			return Result{Action: ActionRepairedRollback, Release: live}, nil
		case ActionRepairedUpgrade:
			r, err := c.Upgrade(ctx, name, chrt, vals)
			return Result{Action: ActionRepairedUpgrade, Release: r}, err
		default: // ActionRepairedReinstall
			if err := c.Uninstall(name); err != nil {
				return Result{}, err
			}
			r, err := c.Install(ctx, name, chrt, vals)
			return Result{Action: ActionRepairedReinstall, Release: r}, err
		}

	case decHold:
		return Result{Action: ActionHeldFailed, Release: live}, nil

	case decWait:
		return Result{Action: ActionWaitingUninstall, Release: live}, nil

	default: // decUnchanged
		return Result{Action: ActionUnchanged, Release: live}, nil
	}
}

// FailureMessage returns a human-readable reason a release is in a failed
// state, preferring Helm's recorded Info.Description (which carries the
// underlying error, e.g. an invalid Service spec) and falling back to the
// given message when no description is available.
func FailureMessage(rel *release.Release, fallback string) string {
	if rel != nil && rel.Info != nil && rel.Info.Description != "" {
		return rel.Info.Description
	}
	return fallback
}

// decision is the internal classification of a release's current state
// relative to desired. EnsureRelease maps it (refining decRepair via
// repairMethod) to a public Action.
type decision int

const (
	decInstall decision = iota
	decUpgrade
	decRepair
	decUnchanged
	decHold
	decWait
)

// classify decides how to converge a release given its live record (or
// absence) and the desired fingerprint.
func classify(live *release.Release, notFound bool, desiredFP string) decision {
	if notFound || live == nil || live.Info == nil {
		return decInstall
	}

	switch live.Info.Status {
	case releasecommon.StatusDeployed:
		if liveFingerprint(live) != desiredFP {
			return decUpgrade
		}
		return decUnchanged

	case releasecommon.StatusFailed,
		releasecommon.StatusPendingInstall,
		releasecommon.StatusPendingUpgrade,
		releasecommon.StatusPendingRollback:
		if liveFingerprint(live) != desiredFP {
			return decRepair
		}
		return decHold

	case releasecommon.StatusUninstalling:
		// A prior uninstall is still in flight (a reconcile raced an
		// in-flight teardown). Installing now would fail with "cannot
		// reuse a name that is still in use"; wait it out so the race
		// self-heals without surfacing a retryable error as ERROR noise.
		return decWait

	default:
		// uninstalled / superseded-as-latest / unknown: no usable
		// release record, so install fresh.
		return decInstall
	}
}

// repairMethod chooses the least-destructive recovery for a release that
// classify flagged as decRepair:
//
//	failed,     deployed revision exists  → upgrade in place
//	pending-*,  deployed revision exists  → rollback (clears the lock; a
//	                                         later upgrade applies desired)
//	failed/pending, no deployed revision  → uninstall + reinstall
//
// A pending-* release is lock-stuck ("another operation in progress"), so
// Helm refuses an in-place upgrade; rollback to the last good revision is
// the non-destructive way out. A release with no deployed revision (failed
// first install) has nothing to upgrade or roll back to, leaving
// uninstall+reinstall as the only path.
func repairMethod(status releasecommon.Status, hasDeployed bool) Action {
	pending := status == releasecommon.StatusPendingInstall ||
		status == releasecommon.StatusPendingUpgrade ||
		status == releasecommon.StatusPendingRollback

	switch {
	case pending && hasDeployed:
		return ActionRepairedRollback
	case hasDeployed:
		return ActionRepairedUpgrade
	default:
		return ActionRepairedReinstall
	}
}

// fingerprint identifies a desired release state: chart version plus a
// stable hash of the values. Two releases with the same fingerprint would
// install the same thing, so re-attempting a failed one is pointless. A
// changed fingerprint means an input changed (new chart, or new rendered
// values from a fixed operator image) and a retry may now succeed.
//
// json.Marshal sorts map keys, so the hash is order-stable; marshalling
// both desired values and a release's stored Config the same way makes them
// comparable despite YAML/JSON type coercion (e.g. int32 and float64 with
// integer value both marshal to "1").
//
// Empty and nil values are normalized to the same form: a component whose
// rendered values are empty installs with map[string]any{} (marshals "{}"),
// but Helm stores that as a nil Config, which reads back and marshals as
// "null". Without this, such a release would look perpetually drifted and be
// upgraded on every reconcile, climbing revisions endlessly.
func fingerprint(chartVersion string, vals map[string]any) string {
	if len(vals) == 0 {
		vals = map[string]any{}
	}
	b, _ := json.Marshal(vals)
	sum := sha256.Sum256(b)
	return chartVersion + ":" + hex.EncodeToString(sum[:])
}

// liveFingerprint computes the fingerprint of an existing release from the
// chart version and values Helm recorded for it.
func liveFingerprint(live *release.Release) string {
	return fingerprint(chartVersion(live.Chart), live.Config)
}

// chartVersion safely reads a chart's version, tolerating nil metadata.
func chartVersion(chrt *chart.Chart) string {
	if chrt == nil || chrt.Metadata == nil {
		return ""
	}
	return chrt.Metadata.Version
}
