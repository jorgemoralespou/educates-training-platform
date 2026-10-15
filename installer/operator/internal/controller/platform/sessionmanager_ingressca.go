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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/pki"
)

// sessionManagerIngressCASecretName is the operator-owned CA trust Secret
// derived from spec.ingressOverrides.caCertificateSecretRef. It lives in
// the operator namespace, holds exactly one key, ca.crt, and replaces the
// cluster's educates-ingress-ca in session-manager's chart values, so the
// override reaches session-manager key-free, like the cluster CA.
const sessionManagerIngressCASecretName = "educates-session-manager-ingress-ca"

// caOverrideField is the spec path of the CA override, used in condition
// messages.
const caOverrideField = "spec.ingressOverrides.caCertificateSecretRef"

// caOverrideRef returns the SessionManager's CA override, or nil when it
// sets none.
func caOverrideRef(obj *platformv1alpha1.SessionManager) *platformv1alpha1.SecretReference {
	if obj.Spec.IngressOverrides == nil {
		return nil
	}
	return obj.Spec.IngressOverrides.CACertificateSecretRef
}

// secretRefNamespace resolves a SecretReference's namespace, where empty
// means the operator namespace.
func (r *SessionManagerReconciler) secretRefNamespace(ref platformv1alpha1.SecretReference) string {
	if ref.Namespace == "" {
		return r.OperatorNamespace
	}
	return ref.Namespace
}

// ensureIngressCAOverride keeps the key-free copy of the CA override in
// step with the spec. With an override it derives
// educates-session-manager-ingress-ca from the override Secret's ca.crt
// alone and server-side applies it, owned by the SessionManager; the
// Secret watch maps the source to the singleton, so the copy follows
// changes to its source. Without one it deletes the copy an earlier
// override left. An override Secret that cannot yield a CA certificate
// returns a *pki.SourceError.
func (r *SessionManagerReconciler) ensureIngressCAOverride(ctx context.Context, obj *platformv1alpha1.SessionManager) error {
	dst := types.NamespacedName{Namespace: r.OperatorNamespace, Name: sessionManagerIngressCASecretName}
	ref := caOverrideRef(obj)
	if ref == nil {
		existing := &corev1.Secret{}
		if err := r.Get(ctx, dst, existing); err != nil {
			return client.IgnoreNotFound(err)
		}
		if !metav1.IsControlledBy(existing, obj) {
			return nil
		}
		return client.IgnoreNotFound(r.Delete(ctx, existing))
	}
	src := pki.CASource{
		Namespace: r.secretRefNamespace(*ref),
		Name:      ref.Name,
		Rule:      pki.FromCATrustSecret,
	}
	// APIReader bypasses the Secret cache, which covers only the
	// namespaces collected at operator startup.
	applier := pki.CATrustApplier{Reader: r.APIReader, Writer: r.Client, Scheme: r.Scheme}
	return applier.Apply(ctx, src, dst, obj)
}

// mapSecretToSessionManager enqueues the SessionManager for the key-free
// copy of its CA override and for the override's source Secret. Every
// other Secret event is dropped.
func (r *SessionManagerReconciler) mapSecretToSessionManager(ctx context.Context, obj client.Object) []reconcile.Request {
	req := []reconcile.Request{{NamespacedName: types.NamespacedName{Name: singletonName}}}
	name, ns := obj.GetName(), obj.GetNamespace()
	if ns == r.OperatorNamespace && name == sessionManagerIngressCASecretName {
		return req
	}
	sm := &platformv1alpha1.SessionManager{}
	if err := r.Get(ctx, types.NamespacedName{Name: singletonName}, sm); err != nil {
		return nil
	}
	if ref := caOverrideRef(sm); ref != nil && ns == r.secretRefNamespace(*ref) && name == ref.Name {
		return req
	}
	return nil
}
