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
	"fmt"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"

	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
)

// handlePlatformReleaseResult maps a helm.EnsureRelease outcome for a platform
// component install to a Reconcile result, shared by the secrets-manager,
// lookup-service, and session-manager reconcilers (each a distinct type, so
// the condition setters are passed as closures). Returns proceed=true when the
// release is converged enough to continue to the Deployment readiness gate.
//
// For the two non-proceeding outcomes it sets the component's conditions via
// markNotReady (which the caller wires to set both Deployed=False and
// Ready=False), advances the phase, persists status, and returns a stop result:
//
//   - ActionHeldFailed: the release is failed and its inputs are unchanged, so
//     a retry would fail identically. Surface the Helm failure and go Degraded
//     instead of reporting Ready off a partial install.
//   - ActionRepairedRollback: a lock-stuck release was rolled back to its last
//     good revision; requeue so the follow-up upgrade applies desired.
func handlePlatformReleaseResult(
	service string,
	res helm.Result,
	markNotReady func(reason, message string),
	setPhase func(phase platformv1alpha1.ComponentPhase),
	persist func() error,
) (proceed bool, result ctrl.Result, err error) {
	switch res.Action {
	case helm.ActionHeldFailed:
		markNotReady("ReleaseFailed", helm.FailureMessage(res.Release, fmt.Sprintf("%s Helm release is in a failed state", service)))
		setPhase(platformv1alpha1.ComponentPhaseDegraded)
		return false, ctrl.Result{}, persist()
	case helm.ActionRepairedRollback:
		markNotReady("RepairingRelease", fmt.Sprintf("rolled %s release back to its last deployed revision; re-applying desired configuration", service))
		setPhase(platformv1alpha1.ComponentPhaseInstalling)
		return false, ctrl.Result{RequeueAfter: 15 * time.Second}, persist()
	default:
		return true, ctrl.Result{}, nil
	}
}
