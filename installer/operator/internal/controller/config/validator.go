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

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

var clusterIssuerGVK = schema.GroupVersionKind{
	Group:   "cert-manager.io",
	Version: "v1",
	Kind:    "ClusterIssuer",
}

// validateInline runs Phase 1 Inline-mode checks against the cluster.
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

	if inline.Ingress.CACertificateSecretRef != nil {
		if err := r.checkCASecret(ctx, inline.Ingress.CACertificateSecretRef.Name); err != nil {
			return nil, err
		}
		out.CACertificateSecretRef = &configv1alpha1.NamespacedSecretRef{
			Namespace: r.OperatorNamespace,
			Name:      inline.Ingress.CACertificateSecretRef.Name,
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

func (r *EducatesClusterConfigReconciler) checkCASecret(ctx context.Context, name string) error {
	s := &corev1.Secret{}
	key := types.NamespacedName{Namespace: r.OperatorNamespace, Name: name}
	if err := r.Get(ctx, key, s); err != nil {
		if apierrors.IsNotFound(err) {
			return &validationError{
				Field:  "spec.inline.ingress.caCertificateSecretRef",
				Reason: fmt.Sprintf("Secret %s/%s not found", r.OperatorNamespace, name),
			}
		}
		return fmt.Errorf("get CA Secret %s: %w", key, err)
	}
	if _, ok := s.Data["ca.crt"]; !ok {
		return &validationError{
			Field:  "spec.inline.ingress.caCertificateSecretRef",
			Reason: fmt.Sprintf("Secret %s/%s is missing required key %q", r.OperatorNamespace, name, "ca.crt"),
		}
	}
	return nil
}

func (r *EducatesClusterConfigReconciler) checkClusterIssuer(ctx context.Context, name string) error {
	ci := &unstructured.Unstructured{}
	ci.SetGroupVersionKind(clusterIssuerGVK)
	if err := r.Get(ctx, types.NamespacedName{Name: name}, ci); err != nil {
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

// isClusterIssuerReady checks for a status condition of type "Ready"
// with status "True" on an unstructured ClusterIssuer object. Returns
// false when the conditions slice is missing, malformed, or carries a
// non-True Ready entry.
func isClusterIssuerReady(ci *unstructured.Unstructured) bool {
	conds, found, err := unstructured.NestedSlice(ci.Object, "status", "conditions")
	if err != nil || !found {
		return false
	}
	for _, c := range conds {
		cMap, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if cMap["type"] == "Ready" && cMap["status"] == "True" {
			return true
		}
	}
	return false
}
