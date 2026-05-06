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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
)

// Phase 0 verification: structural CRD validation only — singleton-name
// CEL on each platform CRD. Reconciler logic is a stub and not under
// test.

func deleteIfExists(obj client.Object, name string) {
	GinkgoHelper()
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, obj); err == nil {
		Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
	}
}

var _ = Describe("SecretsManager CRD validation", func() {
	AfterEach(func() {
		deleteIfExists(&platformv1alpha1.SecretsManager{}, "cluster")
	})

	It("accepts a resource named 'cluster'", func() {
		obj := &platformv1alpha1.SecretsManager{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	})

	It("rejects a resource with a name other than 'cluster' (singleton CEL)", func() {
		obj := &platformv1alpha1.SecretsManager{
			ObjectMeta: metav1.ObjectMeta{Name: "not-cluster"},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("singleton"))
	})
})

var _ = Describe("LookupService CRD validation", func() {
	AfterEach(func() {
		deleteIfExists(&platformv1alpha1.LookupService{}, "cluster")
	})

	It("accepts a resource named 'cluster' with a valid spec", func() {
		obj := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{
					Prefix: "educates-api",
				},
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	})

	It("rejects a resource with a name other than 'cluster' (singleton CEL)", func() {
		obj := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: "not-cluster"},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{
					Prefix: "educates-api",
				},
			},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("singleton"))
	})
})

var _ = Describe("SessionManager CRD validation", func() {
	AfterEach(func() {
		deleteIfExists(&platformv1alpha1.SessionManager{}, "cluster")
	})

	It("accepts a resource named 'cluster'", func() {
		obj := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	})

	It("rejects a resource with a name other than 'cluster' (singleton CEL)", func() {
		obj := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: "not-cluster"},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("singleton"))
	})
})
