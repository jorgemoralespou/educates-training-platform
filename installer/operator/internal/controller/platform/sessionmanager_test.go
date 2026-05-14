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

// makeReadySecretsManager creates the SecretsManager singleton and
// stamps Ready=True directly via the status subresource. Used as a
// fixture for SessionManager specs whose gate-2 depends on it.
func makeReadySecretsManager() *platformv1alpha1.SecretsManager {
	GinkgoHelper()
	sm := &platformv1alpha1.SecretsManager{
		ObjectMeta: metav1.ObjectMeta{Name: singletonName},
	}
	Expect(k8sClient.Create(ctx, sm)).To(Succeed())
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, sm)).To(Succeed())
	sm.Status = platformv1alpha1.SecretsManagerStatus{
		Phase: platformv1alpha1.ComponentPhaseReady,
		Conditions: []metav1.Condition{{
			Type:               conditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             "Reconciled",
			Message:            "test fixture",
			LastTransitionTime: metav1.Now(),
		}},
	}
	Expect(k8sClient.Status().Update(ctx, sm)).To(Succeed())
	return sm
}

func smgrReadyStatus(name string) metav1.ConditionStatus {
	got := &platformv1alpha1.SessionManager{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, got); err != nil {
		return metav1.ConditionUnknown
	}
	c := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if c == nil {
		return metav1.ConditionUnknown
	}
	return c.Status
}

func smgrConditionReason(name, condType string) string {
	got := &platformv1alpha1.SessionManager{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, got); err != nil {
		return ""
	}
	c := meta.FindStatusCondition(got.Status.Conditions, condType)
	if c == nil {
		return ""
	}
	return c.Reason
}

var _ = Describe("SessionManager reconciler (Phase 4 Session 3)", func() {
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

		Expect((&SessionManagerReconciler{
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
		smgr := &platformv1alpha1.SessionManager{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, smgr); err == nil {
			smgr.Finalizers = nil
			_ = k8sClient.Update(ctx, smgr)
			_ = k8sClient.Delete(ctx, smgr)
		}
		_ = k8sClient.DeleteAllOf(ctx, &platformv1alpha1.SecretsManager{})
		_ = k8sClient.DeleteAllOf(ctx, &configv1alpha1.EducatesClusterConfig{})
		_ = k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(platformNamespace))
	})

	It("refuses when EducatesClusterConfig is missing", func() {
		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		Eventually(func() string {
			return smgrConditionReason(singletonName, conditionClusterConfigAvailable)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ClusterConfigNotReady"))

		Expect(smgrReadyStatus(singletonName)).To(Equal(metav1.ConditionFalse))
	})

	It("refuses when SecretsManager is not Ready (ECC Ready, SM missing)", func() {
		_ = makeReadyClusterConfig()

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		// ClusterConfigAvailable should flip True; SecretsManagerAvailable
		// stays False because no SecretsManager CR exists yet.
		Eventually(func() string {
			return smgrConditionReason(singletonName, conditionClusterConfigAvailable)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ClusterConfigReady"))

		Eventually(func() string {
			return smgrConditionReason(singletonName, conditionSecretsManagerAvailable)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("SecretsManagerNotReady"))

		Expect(smgrReadyStatus(singletonName)).To(Equal(metav1.ConditionFalse))
	})

	It("installs the chart and reaches Ready when both gates pass + Deployment Available", func() {
		_ = makeReadyClusterConfig()
		_ = makeReadySecretsManager()

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec:       platformv1alpha1.SessionManagerSpec{},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(sessionManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		markDeploymentAvailable(sessionManagerDeploymentName, platformNamespace)

		Eventually(func() metav1.ConditionStatus {
			return smgrReadyStatus(singletonName)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))

		got := &platformv1alpha1.SessionManager{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(platformv1alpha1.ComponentPhaseReady))
		Expect(got.Status.InstalledVersion).To(Equal(vendoredcharts.SessionManagerChartVersion))
		Expect(got.Status.DeploymentRef).NotTo(BeNil())
		Expect(got.Status.DeploymentRef.Namespace).To(Equal(platformNamespace))
		Expect(got.Status.DeploymentRef.Name).To(Equal(sessionManagerDeploymentName))
	})

	It("uninstalls the chart on delete", func() {
		_ = makeReadyClusterConfig()
		_ = makeReadySecretsManager()
		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(sessionManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(sessionManagerDeploymentName, platformNamespace)
		Eventually(func() metav1.ConditionStatus {
			return smgrReadyStatus(singletonName)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))

		Expect(k8sClient.Delete(ctx, smgr)).To(Succeed())

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, statusErr := hc.Status(sessionManagerReleaseName)
			return statusErr
		}, 30*time.Second, 200*time.Millisecond).Should(MatchError(helm.ErrReleaseNotFound))

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, &platformv1alpha1.SessionManager{})
			return apierrors.IsNotFound(err)
		}, 30*time.Second, 200*time.Millisecond).Should(BeTrue())
	})
})
