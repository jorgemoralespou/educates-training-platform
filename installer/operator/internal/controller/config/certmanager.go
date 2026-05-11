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

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

// fieldManager is the server-side-apply field manager the operator
// asserts ownership under. One constant for the whole operator so SSA
// conflicts surface predictably and `kubectl get -o yaml` shows a
// single owner for operator-created resources.
const fieldManager = "educates-installer"

// Cert-manager Deployment names installed by the upstream chart with
// default values. These are the readiness gate before the operator
// proceeds to ClusterIssuer/Certificate creation.
var certManagerDeployments = []string{
	"cert-manager",
	"cert-manager-webhook",
	"cert-manager-cainjector",
}

// Resource names the operator creates for the wildcard-cert pipeline.
// Constants rather than CR-derived strings so the names are stable
// across reconciles and easy to reference from tests, docs, and the
// finalizer drain path.
const (
	customCASecretName    = "educates-custom-ca"
	wildcardClusterIssuer = "educates-wildcard-issuer"
	wildcardCertificate   = "educates-wildcard"
	wildcardTLSSecretName = "educates-wildcard-tls"
)

// errCertManagerNotReady is returned by ensureCertManagerReady when one
// or more cert-manager Deployments has not yet reported Available=True.
// It is a typed sentinel so the reconciler can map it to a
// CertificatesReady=False/WaitingForWebhook condition without retrying
// the underlying API call.
var errCertManagerNotReady = errors.New("cert-manager Deployments not yet Available")

// ensureCertManagerReady gates the rest of the cert-manager pipeline
// on the three upstream Deployments reporting Available=True. This is
// the Phase 2 readiness contract (decision: Deployment-availability
// only; synthetic admission probe deferred to follow-up — see
// docs/architecture/follow-up-issues.md "Harden cert-manager readiness
// with a synthetic admission probe"). A Deployment that's missing
// (404) maps to "not ready" rather than a hard error — Helm has
// not yet finished applying the manifests in that case.
func (r *EducatesClusterConfigReconciler) ensureCertManagerReady(ctx context.Context) error {
	for _, name := range certManagerDeployments {
		dep := &appsv1.Deployment{}
		key := types.NamespacedName{Namespace: certManagerNamespace, Name: name}
		if err := r.Get(ctx, key, dep); err != nil {
			if apierrors.IsNotFound(err) {
				return errCertManagerNotReady
			}
			return fmt.Errorf("get Deployment %s: %w", key, err)
		}
		if !deploymentAvailable(dep) {
			return errCertManagerNotReady
		}
	}
	return nil
}

// deploymentAvailable reports whether a Deployment has Available=True.
// Returns false when the condition is missing entirely (a fresh
// Deployment whose ReplicaSet is still rolling out).
func deploymentAvailable(d *appsv1.Deployment) bool {
	for _, c := range d.Status.Conditions {
		if c.Type == appsv1.DeploymentAvailable {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// ensureCustomCASecretCopy mirrors the user-supplied CustomCA Secret
// from the operator namespace into the cert-manager namespace so the
// CA-typed ClusterIssuer can read it.
//
// Background: a `kind: ClusterIssuer` with `spec.ca.secretName` reads
// the Secret from cert-manager's `--cluster-resource-namespace` flag,
// which defaults to the namespace cert-manager is installed in
// (cert-manager). The user-supplied Secret lives in the operator
// namespace per the CRD design; the operator owns the copy.
//
// Implementation is SSA so subsequent reconciles converge labels and
// data without read-modify-write races. Owner reference on the copy
// is the EducatesClusterConfig so `kubectl delete educatesclusterconfig`
// cascades the copy.
func (r *EducatesClusterConfigReconciler) ensureCustomCASecretCopy(ctx context.Context, owner *configv1alpha1.EducatesClusterConfig, srcName string) error {
	src := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: r.OperatorNamespace, Name: srcName}, src); err != nil {
		return fmt.Errorf("read source CustomCA Secret %s/%s: %w", r.OperatorNamespace, srcName, err)
	}

	dst := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      customCASecretName,
			Namespace: certManagerNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": managedByLabelValue,
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": src.Data["tls.crt"],
			"tls.key": src.Data["tls.key"],
		},
	}
	if err := controllerSetOwnerOnCrossNamespaceCopy(owner, dst, r.Scheme); err != nil {
		return err
	}

	return r.Patch(ctx, dst, client.Apply, client.FieldOwner(fieldManager), client.ForceOwnership)
}

// controllerSetOwnerOnCrossNamespaceCopy attaches the
// EducatesClusterConfig as a controller owner of a cluster-service
// resource. Owner references can target cluster-scoped owners from
// namespaced dependents directly; controllerutil.SetControllerReference
// happens to enforce a same-namespace check that's wrong for our case
// (cluster-scoped owner → namespaced dependent), so we set the
// reference by hand using metav1.OwnerReference for clarity.
func controllerSetOwnerOnCrossNamespaceCopy(owner *configv1alpha1.EducatesClusterConfig, dst client.Object, scheme *runtime.Scheme) error {
	gvk, err := apiutil.GVKForObject(owner, scheme)
	if err != nil {
		return fmt.Errorf("resolve owner GVK: %w", err)
	}
	ref := metav1.OwnerReference{
		APIVersion:         gvk.GroupVersion().String(),
		Kind:               gvk.Kind,
		Name:               owner.GetName(),
		UID:                owner.GetUID(),
		Controller:         ptrBool(true),
		BlockOwnerDeletion: ptrBool(true),
	}
	dst.SetOwnerReferences([]metav1.OwnerReference{ref})
	return nil
}

func ptrBool(b bool) *bool { return &b }

// ensureClusterIssuer applies the cluster-wide CA-typed Issuer that
// signs the wildcard Certificate. SSA so re-running the reconciler
// converges drift without explicit version tracking.
func (r *EducatesClusterConfigReconciler) ensureClusterIssuer(ctx context.Context, owner *configv1alpha1.EducatesClusterConfig) error {
	ci := &cmv1.ClusterIssuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "ClusterIssuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: wildcardClusterIssuer,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": managedByLabelValue,
			},
		},
		Spec: cmv1.IssuerSpec{
			IssuerConfig: cmv1.IssuerConfig{
				CA: &cmv1.CAIssuer{
					SecretName: customCASecretName,
				},
			},
		},
	}
	if err := controllerSetOwnerOnCrossNamespaceCopy(owner, ci, r.Scheme); err != nil {
		return err
	}
	return r.Patch(ctx, ci, client.Apply, client.FieldOwner(fieldManager), client.ForceOwnership)
}

// ensureWildcardCertificate applies the wildcard Certificate in the
// operator namespace. cert-manager writes the resulting tls Secret
// alongside it in the same namespace, which is where the published
// status.ingress.wildcardCertificateSecretRef points. dnsNames cover
// both `<domain>` and `*.<domain>` so the same cert handles the
// portal hostname and per-session hostnames.
func (r *EducatesClusterConfigReconciler) ensureWildcardCertificate(ctx context.Context, owner *configv1alpha1.EducatesClusterConfig, domain string) error {
	cert := &cmv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      wildcardCertificate,
			Namespace: r.OperatorNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": managedByLabelValue,
			},
		},
		Spec: cmv1.CertificateSpec{
			SecretName: wildcardTLSSecretName,
			DNSNames:   []string{domain, "*." + domain},
			IssuerRef: cmmeta.ObjectReference{
				Kind: "ClusterIssuer",
				Name: wildcardClusterIssuer,
			},
		},
	}
	if err := controllerSetOwnerOnCrossNamespaceCopy(owner, cert, r.Scheme); err != nil {
		return err
	}
	return r.Patch(ctx, cert, client.Apply, client.FieldOwner(fieldManager), client.ForceOwnership)
}

// certificateReady reports whether the wildcard Certificate carries
// Ready=True. Returns (false, nil) when the Certificate or its Ready
// condition is missing (still being issued).
func (r *EducatesClusterConfigReconciler) certificateReady(ctx context.Context) (bool, error) {
	cert := &cmv1.Certificate{}
	key := types.NamespacedName{Namespace: r.OperatorNamespace, Name: wildcardCertificate}
	if err := r.Get(ctx, key, cert); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get wildcard Certificate %s: %w", key, err)
	}
	for _, c := range cert.Status.Conditions {
		if c.Type == cmv1.CertificateConditionReady {
			return c.Status == cmmeta.ConditionTrue, nil
		}
	}
	return false, nil
}
