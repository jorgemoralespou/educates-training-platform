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
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/pki"
)

// Spec paths of the wildcard certificate references, used in condition
// messages and cache-miss warnings.
const (
	staticCertificateField = "spec.ingress.certificates.staticCertificate"
	staticTLSField         = "spec.ingress.certificates.staticCertificate.tlsSecretRef"
	inlineWildcardField    = "spec.inline.ingress.wildcardCertificateSecretRef"
)

// reasonStaticCertificateValid is the CertificatesReady reason once the
// StaticCertificate provider serves a certificate that passed the check.
const reasonStaticCertificateValid = "StaticCertificateValid"

// expiryRequeueMargin is added to the time left before a certificate
// expires, so the reconcile it schedules runs once the certificate has
// expired rather than at its last valid instant.
const expiryRequeueMargin = time.Second

// staticCertificateConfig returns the Managed-mode StaticCertificate
// configuration, or nil when the spec does not serve a static
// certificate.
func staticCertificateConfig(obj *configv1alpha1.EducatesClusterConfig) *configv1alpha1.StaticCertificateConfig {
	if obj.Spec.Mode != configv1alpha1.ClusterConfigModeManaged || obj.Spec.Ingress == nil {
		return nil
	}
	certs := obj.Spec.Ingress.Certificates
	if certs.Provider != configv1alpha1.CertificatesProviderStaticCertificate {
		return nil
	}
	return certs.StaticCertificate
}

// checkWildcardCertificate reads the wildcard certificate Secret ref
// names and checks Educates can serve it for domain. When caRef is set,
// the certificate's chain must verify against that CA trust Secret's
// ca.crt. StaticCertificate and Inline mode share it, so both report a
// problem with the same CertificatesReady reason. The Secrets are read
// through APIReader, so a namespace outside the Secret cache still
// reads.
func (r *EducatesClusterConfigReconciler) checkWildcardCertificate(
	ctx context.Context,
	ref configv1alpha1.SecretReference, field, domain string,
	caRef *configv1alpha1.SecretReference, caField string,
) (*pki.WildcardCertificate, error) {
	want := pki.WildcardRequirements{Domain: domain, Now: time.Now()}
	if caRef != nil {
		src := r.caTrustSource(*caRef)
		r.warnIfCacheMiss(ctx, src.Namespace, caField)
		caPEM, err := pki.ReadCACertificate(ctx, r.APIReader, src)
		if err != nil {
			return nil, asCertificatesValidationError(err, caField)
		}
		want.CA = caPEM
	}
	key := types.NamespacedName{Namespace: r.secretRefNamespace(ref), Name: ref.Name}
	r.warnIfCacheMiss(ctx, key.Namespace, field)
	cert, err := pki.ReadWildcardCertificate(ctx, r.APIReader, key, want)
	if err != nil {
		return nil, asCertificatesValidationError(err, field)
	}
	return cert, nil
}

// reconcileStaticCertificatePhase is the certificates phase of the
// StaticCertificate provider. No cert-manager, ClusterIssuer or
// Certificate is involved: it checks the referenced wildcard
// certificate, copies only its tls.crt and tls.key into the
// operator-owned educates-wildcard-tls, and marks CertificatesReady.
// The Secret watch maps the referenced Secrets to the singleton, so the
// copy follows its source.
//
// A certificate that fails the check is a validation failure with the
// check's CertificatesReady reason. On success the phase is done and
// its Result requeues once the certificate expires, so status reports
// the expiry without any other event.
func (r *EducatesClusterConfigReconciler) reconcileStaticCertificatePhase(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) (bool, ctrl.Result, error) {
	static := obj.Spec.Ingress.Certificates.StaticCertificate
	cert, err := r.checkWildcardCertificate(ctx,
		static.TLSSecretRef, staticTLSField, obj.Spec.Ingress.Domain,
		static.CACertificateSecretRef, staticCATrustField)
	if err != nil {
		if verr, ok := errors.AsType[*validationError](err); ok {
			r.markValidationFailed(obj, verr)
			return false, ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, obj)
		}
		return false, ctrl.Result{}, err
	}

	if err := r.ensureStaticWildcardSecret(ctx, obj, cert); err != nil {
		r.markCertificatesProgressing(obj, "WildcardSecretApplyFailed", err.Error())
		_ = r.updateStatusWithTransitionLog(ctx, obj)
		return false, ctrl.Result{}, err
	}

	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:   conditionCertificatesReady,
		Status: metav1.ConditionTrue,
		Reason: reasonStaticCertificateValid,
		Message: fmt.Sprintf("serving the wildcard certificate from Secret %s/%s, valid until %s",
			r.secretRefNamespace(static.TLSSecretRef), static.TLSSecretRef.Name,
			cert.NotAfter.UTC().Format(time.RFC3339)),
		ObservedGeneration: obj.Generation,
	})
	return true, ctrl.Result{RequeueAfter: time.Until(cert.NotAfter) + expiryRequeueMargin}, nil
}

// ensureStaticWildcardSecret server-side applies educates-wildcard-tls
// in the operator namespace with only the checked certificate's tls.crt
// and tls.key, owned by the EducatesClusterConfig so it is garbage
// collected with it.
func (r *EducatesClusterConfigReconciler) ensureStaticWildcardSecret(ctx context.Context, owner *configv1alpha1.EducatesClusterConfig, cert *pki.WildcardCertificate) error {
	dst := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      wildcardTLSSecretName,
			Namespace: r.OperatorNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": managedByLabelValue,
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			corev1.TLSCertKey:       cert.Certificate,
			corev1.TLSPrivateKeyKey: cert.PrivateKey,
		},
	}
	if err := controllerSetOwnerOnCrossNamespaceCopy(owner, dst, r.Scheme); err != nil {
		return err
	}
	if err := r.applySSA(ctx, dst); err != nil {
		return fmt.Errorf("apply wildcard Secret %s/%s: %w", r.OperatorNamespace, wildcardTLSSecretName, err)
	}
	return nil
}

// requeueBy returns res, rescheduled to run no later than after when
// after is positive.
func requeueBy(res ctrl.Result, after time.Duration) ctrl.Result {
	if after > 0 && (res.RequeueAfter <= 0 || after < res.RequeueAfter) {
		res.RequeueAfter = after
	}
	return res
}
