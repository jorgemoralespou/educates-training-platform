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

	// ConditionType and ConditionReason, when set, also report the
	// failure on a per-concern condition (such as CertificatesReady)
	// with a machine-readable reason, so a reader can tell failures
	// apart without parsing the message.
	ConditionType   string
	ConditionReason string
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

	out := &configv1alpha1.StatusIngress{
		Domain:           inline.Ingress.Domain,
		IngressClassName: inline.Ingress.IngressClassName,
		Protocol:         resolveInlineProtocol(inline.Ingress),
	}

	// No wildcard certificate means plain HTTP or TLS terminated outside
	// the cluster; components read the absent ref accordingly.
	if ref := inline.Ingress.WildcardCertificateSecretRef; ref != nil {
		if err := r.checkWildcardSecret(ctx, *ref); err != nil {
			return nil, err
		}
		out.WildcardCertificateSecretRef = &configv1alpha1.NamespacedSecretRef{
			Namespace: r.secretRefNamespace(*ref),
			Name:      ref.Name,
		}
	}

	// The CA Secret itself is never published: components receive only
	// the key-free educates-ingress-ca that ensureIngressCATrust derives
	// from it, which also reports a CA Secret it cannot use.
	if inline.Ingress.CACertificateSecretRef != nil {
		out.CACertificateSecretRef = r.ingressCASecretRef()
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

// resolveInlineProtocol returns the public-URL scheme to publish for
// Inline mode. An explicit ingress.protocol wins; otherwise it derives
// from the wildcard certificate: https when one is declared, http
// otherwise.
func resolveInlineProtocol(ingress configv1alpha1.InlineIngress) configv1alpha1.IngressProtocol {
	if ingress.Protocol != "" {
		return ingress.Protocol
	}
	if ingress.WildcardCertificateSecretRef != nil {
		return configv1alpha1.IngressProtocolHTTPS
	}
	return configv1alpha1.IngressProtocolHTTP
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

// checkWildcardSecret validates the Inline-mode
// wildcardCertificateSecretRef: the Secret exists in the referenced
// namespace and holds tls.crt and tls.key. It reads through APIReader,
// so a namespace outside the Secret cache scope still reads.
func (r *EducatesClusterConfigReconciler) checkWildcardSecret(ctx context.Context, ref configv1alpha1.SecretReference) error {
	const field = "spec.inline.ingress.wildcardCertificateSecretRef"
	key := types.NamespacedName{Namespace: r.secretRefNamespace(ref), Name: ref.Name}
	r.warnIfCacheMiss(ctx, key.Namespace, field)
	s := &corev1.Secret{}
	if err := r.APIReader.Get(ctx, key, s); err != nil {
		if apierrors.IsNotFound(err) {
			return &validationError{
				Field:  field,
				Reason: fmt.Sprintf("Secret %s not found", key),
			}
		}
		return fmt.Errorf("get wildcard Secret %s: %w", key, err)
	}
	for _, k := range []string{"tls.crt", "tls.key"} {
		if _, ok := s.Data[k]; !ok {
			return &validationError{
				Field:  field,
				Reason: fmt.Sprintf("Secret %s is missing required key %q", key, k),
			}
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
