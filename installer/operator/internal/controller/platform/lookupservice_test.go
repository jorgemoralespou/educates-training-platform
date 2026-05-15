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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

func lsReadyStatus(name string) metav1.ConditionStatus {
	got := &platformv1alpha1.LookupService{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, got); err != nil {
		return metav1.ConditionUnknown
	}
	c := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if c == nil {
		return metav1.ConditionUnknown
	}
	return c.Status
}

var _ = Describe("LookupService reconciler (Phase 4 Session 2)", func() {
	var (
		mgrCancel context.CancelFunc
		mgrDone   chan error
		helmFac   *memoryHelmFactory
	)

	BeforeEach(func() {
		ensureNamespace(platformNamespace)
		helmFac = newMemoryHelmFactory()

		var mgrCtx context.Context
		mgrCtx, mgrCancel = context.WithCancel(ctx)

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:     k8sClient.Scheme(),
			Metrics:    metricsserver.Options{BindAddress: "0"},
			Controller: crconfig.Controller{SkipNameValidation: ptr.To(true)},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect((&LookupServiceReconciler{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			HelmClientFor: helmFac.For,
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
		ls := &platformv1alpha1.LookupService{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, ls); err == nil {
			ls.Finalizers = nil
			_ = k8sClient.Update(ctx, ls)
			_ = k8sClient.Delete(ctx, ls)
		}
		_ = k8sClient.DeleteAllOf(ctx, &platformv1alpha1.SecretsManager{})
		_ = k8sClient.DeleteAllOf(ctx, &configv1alpha1.EducatesClusterConfig{})
		_ = k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(platformNamespace))
	})

	It("refuses to proceed when EducatesClusterConfig is missing", func() {
		ls := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		}
		Expect(k8sClient.Create(ctx, ls)).To(Succeed())

		Eventually(func() string {
			got := &platformv1alpha1.LookupService{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got); err != nil {
				return ""
			}
			c := meta.FindStatusCondition(got.Status.Conditions, conditionClusterConfigAvailable)
			if c == nil {
				return ""
			}
			return c.Reason
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ClusterConfigNotReady"))

		Expect(lsReadyStatus(singletonName)).To(Equal(metav1.ConditionFalse))
	})

	It("installs the chart, derives status.url, and reaches Ready", func() {
		_ = makeReadyClusterConfig()
		_ = makeReadySecretsManager()

		ls := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		}
		Expect(k8sClient.Create(ctx, ls)).To(Succeed())

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(lookupServiceReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		markDeploymentAvailable(lookupServiceDeploymentName, platformNamespace)

		Eventually(func() metav1.ConditionStatus {
			return lsReadyStatus(singletonName)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))

		got := &platformv1alpha1.LookupService{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(platformv1alpha1.ComponentPhaseReady))
		Expect(got.Status.InstalledVersion).To(Equal(vendoredcharts.LookupServiceChartVersion))
		Expect(got.Status.URL).To(Equal("https://lookup.test.example.com"))
		Expect(got.Status.DeploymentRef).NotTo(BeNil())
		Expect(got.Status.DeploymentRef.Namespace).To(Equal(platformNamespace))
		Expect(got.Status.DeploymentRef.Name).To(Equal(lookupServiceDeploymentName))
	})

	It("uninstalls the chart on delete", func() {
		_ = makeReadyClusterConfig()
		_ = makeReadySecretsManager()
		ls := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		}
		Expect(k8sClient.Create(ctx, ls)).To(Succeed())

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(lookupServiceReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(lookupServiceDeploymentName, platformNamespace)
		Eventually(func() metav1.ConditionStatus {
			return lsReadyStatus(singletonName)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))

		Expect(k8sClient.Delete(ctx, ls)).To(Succeed())

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, statusErr := hc.Status(lookupServiceReleaseName)
			return statusErr
		}, 30*time.Second, 200*time.Millisecond).Should(MatchError(helm.ErrReleaseNotFound))

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, &platformv1alpha1.LookupService{})
			return apierrors.IsNotFound(err)
		}, 30*time.Second, 200*time.Millisecond).Should(BeTrue())
	})
})
