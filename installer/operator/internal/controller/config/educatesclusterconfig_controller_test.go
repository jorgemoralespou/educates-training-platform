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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

// Phase 0 verification: structural CRD validation only. Reconciler logic
// is a stub (logs and returns) and not under test here.

var _ = Describe("EducatesClusterConfig CRD validation", func() {
	AfterEach(func() {
		// Clean up the singleton if a prior test created it; ignore not-found.
		obj := &configv1alpha1.EducatesClusterConfig{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, obj); err == nil {
			Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
		}
	})

	It("accepts a Managed-mode resource named 'cluster'", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	})

	It("rejects a resource with a name other than 'cluster' (singleton CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "not-cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
			},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("singleton"))
	})

	It("rejects a spec.mode change on update (mode immutability CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.Mode = configv1alpha1.ClusterConfigModeInline
		err := k8sClient.Update(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("immutable"))
	})

	It("rejects Managed-mode fields when mode is Inline (exclusivity CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
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
				DNS: &configv1alpha1.DNS{
					Provider: configv1alpha1.DNSProviderNone,
				},
			},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("forbidden when mode is Inline"))
	})

	It("rejects spec.inline when mode is Managed (exclusivity CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
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
			},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.inline is forbidden"))
	})
})
