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

package config

import (
	"context"
	"fmt"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

// validationError carries a field path and human-readable reason so
// status-condition messages can name the offending input. It is the
// only error type validateInline returns when the validation outcome
// is "user input is wrong" rather than "I couldn't talk to the API".
type validationError struct {
	Field  string
	Reason string
}

func (e *validationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Reason)
}

// validateInline runs Inline-mode checks against the cluster.
// On success it returns a populated StatusIngress ready to publish; on
// validation failure (referenced object missing, missing key, not
// Ready) it returns a *validationError. Any other error means the API
// call itself failed and reconciliation should be retried.
func (r *EducatesClusterConfigReconciler) validateInline(ctx context.Context, inline *configv1alpha1.InlineConfig) (*configv1alpha1.StatusIngress, error) {
	if err := r.checkIngressClass(ctx, inline.Ingress.IngressClassName); err != nil {
		return nil, err
	}

	if err := r.checkWildcardSecret(ctx, inline.Ingress.WildcardCertificateSecretRef.Name); err != nil {
		return nil, err
	}

	out := &configv1alpha1.StatusIngress{
		Domain:           inline.Ingress.Domain,
		IngressClassName: inline.Ingress.IngressClassName,
		WildcardCertificateSecretRef: configv1alpha1.NamespacedSecretRef{
			Namespace: r.OperatorNamespace,
			Name:      inline.Ingress.WildcardCertificateSecretRef.Name,
		},
	}

	if ref := inline.Ingress.CACertificateSecretRef; ref != nil {
		if err := r.checkCASecret(ctx, *ref); err != nil {
			return nil, err
		}
		ns := ref.Namespace
		if ns == "" {
			ns = r.OperatorNamespace
		}
		out.CACertificateSecretRef = &configv1alpha1.NamespacedSecretRef{
			Namespace: ns,
			Name:      ref.Name,
		}
	}

	if inline.Ingress.ClusterIssuerRef != nil {
		if err := r.checkClusterIssuer(ctx, inline.Ingress.ClusterIssuerRef.Name); err != nil {
			return nil, err
		}
		out.ClusterIssuerRef = &configv1alpha1.LocalObjectReference{
			Name: inline.Ingress.ClusterIssuerRef.Name,
		}
	}

	return out, nil
}

func (r *EducatesClusterConfigReconciler) checkIngressClass(ctx context.Context, name string) error {
	ic := &networkingv1.IngressClass{}
	if err := r.Get(ctx, types.NamespacedName{Name: name}, ic); err != nil {
		if apierrors.IsNotFound(err) {
			return &validationError{
				Field:  "spec.inline.ingress.ingressClassName",
				Reason: fmt.Sprintf("IngressClass %q not found", name),
			}
		}
		return fmt.Errorf("get IngressClass %q: %w", name, err)
	}
	return nil
}

func (r *EducatesClusterConfigReconciler) checkWildcardSecret(ctx context.Context, name string) error {
	s := &corev1.Secret{}
	key := types.NamespacedName{Namespace: r.OperatorNamespace, Name: name}
	if err := r.Get(ctx, key, s); err != nil {
		if apierrors.IsNotFound(err) {
			return &validationError{
				Field:  "spec.inline.ingress.wildcardCertificateSecretRef",
				Reason: fmt.Sprintf("Secret %s/%s not found", r.OperatorNamespace, name),
			}
		}
		return fmt.Errorf("get wildcard Secret %s: %w", key, err)
	}
	for _, k := range []string{"tls.crt", "tls.key"} {
		if _, ok := s.Data[k]; !ok {
			return &validationError{
				Field:  "spec.inline.ingress.wildcardCertificateSecretRef",
				Reason: fmt.Sprintf("Secret %s/%s is missing required key %q", r.OperatorNamespace, name, k),
			}
		}
	}
	return nil
}

// checkCASecret validates the Inline-mode caCertificateSecretRef. The
// ref's optional Namespace is honoured (defaulting to the operator
// namespace when empty) — mirrors the Managed-mode CustomCA flow's
// CASecretReference semantics. Bypasses the cache via APIReader so
// cross-namespace reads (e.g. educates-secrets) don't fail with
// "unknown namespace for the cache".
func (r *EducatesClusterConfigReconciler) checkCASecret(ctx context.Context, ref configv1alpha1.CASecretReference) error {
	ns := ref.Namespace
	if ns == "" {
		ns = r.OperatorNamespace
	}
	r.warnIfCacheMiss(ctx, ns, "spec.inline.ingress.caCertificateSecretRef")
	s := &corev1.Secret{}
	key := types.NamespacedName{Namespace: ns, Name: ref.Name}
	if err := r.APIReader.Get(ctx, key, s); err != nil {
		if apierrors.IsNotFound(err) {
			return &validationError{
				Field:  "spec.inline.ingress.caCertificateSecretRef",
				Reason: fmt.Sprintf("Secret %s/%s not found", ns, ref.Name),
			}
		}
		return fmt.Errorf("get CA Secret %s: %w", key, err)
	}
	if _, ok := s.Data["ca.crt"]; !ok {
		return &validationError{
			Field:  "spec.inline.ingress.caCertificateSecretRef",
			Reason: fmt.Sprintf("Secret %s/%s is missing required key %q", ns, ref.Name, "ca.crt"),
		}
	}
	return nil
}

func (r *EducatesClusterConfigReconciler) checkClusterIssuer(ctx context.Context, name string) error {
	ci := &cmv1.ClusterIssuer{}
	// Bypass the cache via APIReader. The ClusterIssuer watch is a
	// deferred (unstructured) informer registered by CRDWatcher, whereas
	// a cached typed Get here would hit a *separate* informer keyed on
	// *cmv1.ClusterIssuer. The two caches sync independently, so on a
	// ClusterIssuer deletion the watch can fire the reconcile while the
	// typed cache still returns the stale object — leaving status wedged
	// at Ready and nothing re-triggering afterward. A direct read sees
	// the deletion immediately.
	if err := r.APIReader.Get(ctx, types.NamespacedName{Name: name}, ci); err != nil {
		// IsNoMatchError covers the "cert-manager CRD not installed"
		// case — surface it as a validation error rather than a
		// reconcile retry, since the user can fix it.
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return &validationError{
				Field:  "spec.inline.ingress.clusterIssuerRef",
				Reason: fmt.Sprintf("ClusterIssuer %q not found (or cert-manager not installed)", name),
			}
		}
		return fmt.Errorf("get ClusterIssuer %q: %w", name, err)
	}
	if !isClusterIssuerReady(ci) {
		return &validationError{
			Field:  "spec.inline.ingress.clusterIssuerRef",
			Reason: fmt.Sprintf("ClusterIssuer %q is not Ready", name),
		}
	}
	return nil
}

// isClusterIssuerReady reports whether the ClusterIssuer carries a
// Ready=True condition. Returns false when the conditions slice is empty
// or carries a non-True Ready entry.
func isClusterIssuerReady(ci *cmv1.ClusterIssuer) bool {
	for _, c := range ci.Status.Conditions {
		if c.Type == cmv1.IssuerConditionReady && c.Status == cmmeta.ConditionTrue {
			return true
		}
	}
	return false
}
