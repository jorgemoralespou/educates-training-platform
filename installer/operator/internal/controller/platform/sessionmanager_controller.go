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
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
)

// SessionManagerReconciler reconciles a SessionManager object
type SessionManagerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=platform.educates.dev,resources=sessionmanagers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.educates.dev,resources=sessionmanagers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.educates.dev,resources=sessionmanagers/finalizers,verbs=update

// Reconcile is the entry point for the SessionManager controller.
//
// Phase 0: stub. Logs the observed object and returns without making any
// state changes. Real reconciliation lands in Phase 4.
func (r *SessionManagerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling SessionManager", "name", req.Name)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SessionManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.SessionManager{}).
		Named("platform-sessionmanager").
		Complete(r)
}
