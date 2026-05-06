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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

// readyConditionStatus returns the Ready condition's status, or
// ConditionUnknown if the resource or condition is missing. Used as
// the polling target for Eventually().
func readyConditionStatus() metav1.ConditionStatus {
	got := &configv1alpha1.EducatesClusterConfig{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
		return metav1.ConditionUnknown
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if cond == nil {
		return metav1.ConditionUnknown
	}
	return cond.Status
}

var _ = Describe("EducatesClusterConfig watches (manager-driven)", func() {
	var mgrCancel context.CancelFunc
	var mgrDone chan error

	BeforeEach(func() {
		ensureNamespace(testOperatorNamespace)

		var mgrCtx context.Context
		mgrCtx, mgrCancel = context.WithCancel(ctx)

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: k8sClient.Scheme(),
			Cache: cache.Options{
				ByObject: map[client.Object]cache.ByObject{
					&corev1.Secret{}: {
						Namespaces: map[string]cache.Config{
							testOperatorNamespace: {},
						},
					},
				},
			},
			// Disable the metrics server in-test; envtest doesn't need it
			// and binding a port can collide across specs.
			Metrics: metricsserver.Options{BindAddress: "0"},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect((&EducatesClusterConfigReconciler{
			Client:            mgr.GetClient(),
			Scheme:            mgr.GetScheme(),
			OperatorNamespace: testOperatorNamespace,
		}).SetupWithManager(mgr)).To(Succeed())

		mgrDone = make(chan error, 1)
		go func() {
			defer GinkgoRecover()
			mgrDone <- mgr.Start(mgrCtx)
		}()
	})

	AfterEach(func() {
		mgrCancel()
		Eventually(mgrDone, 10*time.Second).Should(Receive())
		drainCR()
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(testOperatorNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &networkingv1.IngressClass{})
	})

	It("flips status from Ready to Degraded when the wildcard Secret is deleted", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass("contour"))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret("wildcard-tls", true, true))).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionTrue), "expected Ready=True after initial reconcile")

		// Delete the Secret. The Secret watch should map back to the
		// singleton Reconcile, which finds the missing Secret and writes
		// Degraded.
		Expect(k8sClient.Delete(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "wildcard-tls", Namespace: testOperatorNamespace},
		})).To(Succeed())

		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionFalse), "expected Ready=False after Secret deletion")
	})
})
