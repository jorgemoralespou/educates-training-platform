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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

const testOperatorNamespace = "test-operator"

// reconcileTwice runs Reconcile once to add the finalizer (first call
// returns Requeue) and once more to write status. Phase 1 reconciler
// behaviour: real users hit the same two-pass sequence via the
// controller-runtime queue.
func reconcileTwice(r *EducatesClusterConfigReconciler) {
	GinkgoHelper()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "cluster"}}
	_, err := r.Reconcile(ctx, req)
	Expect(err).NotTo(HaveOccurred())
	_, err = r.Reconcile(ctx, req)
	Expect(err).NotTo(HaveOccurred())
}

// drainCR removes the finalizer (so Delete actually deletes), deletes
// the CR, and waits until it's gone. Used in AfterEach because the
// Phase 1 reconciler uses finalizers but we don't run a manager during
// envtest.
func drainCR() {
	GinkgoHelper()
	obj := &configv1alpha1.EducatesClusterConfig{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return
		}
		Expect(err).NotTo(HaveOccurred())
	}
	obj.Finalizers = nil
	Expect(k8sClient.Update(ctx, obj)).To(Succeed())
	Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, obj)
		return apierrors.IsNotFound(err)
	}).Should(BeTrue())
}

func makeReconciler() *EducatesClusterConfigReconciler {
	return &EducatesClusterConfigReconciler{
		Client:            k8sClient,
		Scheme:            k8sClient.Scheme(),
		OperatorNamespace: testOperatorNamespace,
	}
}

func validInlineSpec() configv1alpha1.EducatesClusterConfigSpec {
	return configv1alpha1.EducatesClusterConfigSpec{
		Mode: configv1alpha1.ClusterConfigModeInline,
		Inline: &configv1alpha1.InlineConfig{
			Ingress: configv1alpha1.InlineIngress{
				Domain:           "educates.test",
				IngressClassName: "contour",
				WildcardCertificateSecretRef: configv1alpha1.LocalObjectReference{
					Name: "wildcard-tls",
				},
			},
			PolicyEnforcement: configv1alpha1.InlinePolicyEnforcement{
				ClusterPolicyEngine:  configv1alpha1.ClusterPolicyEngineKyverno,
				WorkshopPolicyEngine: configv1alpha1.WorkshopPolicyEngineKyverno,
			},
		},
	}
}

func ensureNamespace(name string) {
	GinkgoHelper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	err := k8sClient.Create(ctx, ns)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func makeIngressClass(name string) *networkingv1.IngressClass {
	return &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: networkingv1.IngressClassSpec{
			Controller: "test/example",
		},
	}
}

func makeWildcardSecret(name string, withTLSCrt, withTLSKey bool) *corev1.Secret {
	data := map[string][]byte{}
	if withTLSCrt {
		data["tls.crt"] = []byte("dummy-cert")
	}
	if withTLSKey {
		data["tls.key"] = []byte("dummy-key")
	}
	// Type intentionally Opaque — kubernetes.io/tls Secrets are
	// apiserver-validated to require both tls.crt + tls.key, which would
	// block tests that exercise the "missing key" validator path.
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testOperatorNamespace},
		Data:       data,
	}
}

var _ = Describe("EducatesClusterConfig Inline-mode reconciler", func() {
	BeforeEach(func() {
		ensureNamespace(testOperatorNamespace)
	})

	AfterEach(func() {
		drainCR()
		// Best-effort cleanup of supporting resources; ignore not-found.
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(testOperatorNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &networkingv1.IngressClass{})
	})

	It("flips to Ready and publishes status when all refs validate", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass("contour"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret("wildcard-tls", true, true))).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())

		Expect(got.Finalizers).To(ContainElement(finalizerName))
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseReady))
		Expect(got.Status.Mode).To(Equal(configv1alpha1.ClusterConfigModeInline))
		Expect(got.Status.Ingress).NotTo(BeNil())
		Expect(got.Status.Ingress.Domain).To(Equal("educates.test"))
		Expect(got.Status.Ingress.IngressClassName).To(Equal("contour"))
		Expect(got.Status.Ingress.WildcardCertificateSecretRef.Namespace).To(Equal(testOperatorNamespace))
		Expect(got.Status.Ingress.WildcardCertificateSecretRef.Name).To(Equal("wildcard-tls"))
		Expect(got.Status.PolicyEnforcement).NotTo(BeNil())
		Expect(got.Status.PolicyEnforcement.ClusterPolicyEngine).To(Equal(configv1alpha1.ClusterPolicyEngineKyverno))
		Expect(got.Status.ImageRegistry).NotTo(BeNil()) // empty but populated

		ready := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))

		val := meta.FindStatusCondition(got.Status.Conditions, conditionValidationSucceeded)
		Expect(val).NotTo(BeNil())
		Expect(val.Status).To(Equal(metav1.ConditionTrue))
	})

	It("flips to Degraded when the wildcard Secret is missing", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass("contour"))).To(Succeed())
		// No wildcard Secret created.

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))

		ready := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Message).To(ContainSubstring("wildcardCertificateSecretRef"))
		Expect(ready.Message).To(ContainSubstring("not found"))
	})

	It("flips to Degraded when the wildcard Secret is missing tls.crt", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass("contour"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret("wildcard-tls", false, true))).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))

		ready := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Message).To(ContainSubstring(`"tls.crt"`))
	})

	It("flips to Degraded when the IngressClass is missing", func() {
		// No IngressClass created.
		Expect(k8sClient.Create(ctx, makeWildcardSecret("wildcard-tls", true, true))).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))

		ready := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(ready.Message).To(ContainSubstring("IngressClass"))
		Expect(ready.Message).To(ContainSubstring(`"contour"`))
	})

	It("flips to Degraded when an optional CA Secret is referenced but missing", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass("contour"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret("wildcard-tls", true, true))).To(Succeed())

		spec := validInlineSpec()
		spec.Inline.Ingress.CACertificateSecretRef = &configv1alpha1.LocalObjectReference{Name: "ca-bundle"}

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))

		ready := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(ready.Message).To(ContainSubstring("caCertificateSecretRef"))
	})

	It("clears the finalizer on delete", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass("contour"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret("wildcard-tls", true, true))).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		reconcileTwice(makeReconciler())

		// Mark for deletion: the finalizer keeps it around until the
		// reconciler explicitly removes it.
		Expect(k8sClient.Delete(ctx, obj)).To(Succeed())

		// One more Reconcile pass: should drain the finalizer and let
		// the apiserver remove the object.
		_, err := makeReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "cluster"}})
		Expect(err).NotTo(HaveOccurred())

		got := &configv1alpha1.EducatesClusterConfig{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
})
