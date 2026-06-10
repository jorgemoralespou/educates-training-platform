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

	release "helm.sh/helm/v4/pkg/release/v1"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
func makeReadySecretsManager() {
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
}

func smgrReadyStatus() metav1.ConditionStatus {
	name := singletonName
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

func smgrConditionReason(condType string) string {
	name := singletonName
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
			Controller: crconfig.Controller{SkipNameValidation: new(true)},
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
		// LookupService specs create the singleton as part of the
		// remote-access Auto-presence-signal coverage; drain it so
		// the next spec starts with a clean cluster.
		ls := &platformv1alpha1.LookupService{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, ls); err == nil {
			ls.Finalizers = nil
			_ = k8sClient.Update(ctx, ls)
			_ = k8sClient.Delete(ctx, ls)
		}
		_ = k8sClient.DeleteAllOf(ctx, &configv1alpha1.EducatesClusterConfig{})
		_ = k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(platformNamespace))
	})

	It("refuses when EducatesClusterConfig is missing", func() {
		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		Eventually(func() string {
			return smgrConditionReason(conditionClusterConfigAvailable)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ClusterConfigNotReady"))

		Expect(smgrReadyStatus()).To(Equal(metav1.ConditionFalse))
	})

	It("refuses when SecretsManager is not Ready (ECC Ready, SM missing)", func() {
		makeReadyClusterConfig()

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		// ClusterConfigAvailable should flip True; SecretsManagerAvailable
		// stays False because no SecretsManager CR exists yet.
		Eventually(func() string {
			return smgrConditionReason(conditionClusterConfigAvailable)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ClusterConfigReady"))

		Eventually(func() string {
			return smgrConditionReason(conditionSecretsManagerAvailable)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("SecretsManagerNotReady"))

		Expect(smgrReadyStatus()).To(Equal(metav1.ConditionFalse))
	})

	It("installs the chart and reaches Ready when both gates pass + Deployment Available", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

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

		markDeploymentAvailable(sessionManagerDeploymentName)

		Eventually(func() metav1.ConditionStatus {
			return smgrReadyStatus()
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))

		got := &platformv1alpha1.SessionManager{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(platformv1alpha1.ComponentPhaseReady))
		Expect(got.Status.InstalledVersion).To(Equal(vendoredcharts.SessionManagerChartVersion))
		Expect(got.Status.DeploymentRef).NotTo(BeNil())
		Expect(got.Status.DeploymentRef.Namespace).To(Equal(platformNamespace))
		Expect(got.Status.DeploymentRef.Name).To(Equal(sessionManagerDeploymentName))
	})

	It("renders Secret-sourced themes and the imagePrePuller toggle into chart values", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.SessionManagerSpec{
				Themes: []platformv1alpha1.Theme{
					{
						Name: "corporate",
						Source: platformv1alpha1.ThemeSource{
							Type: platformv1alpha1.ThemeSourceTypeSecret,
							SecretRef: &platformv1alpha1.NamespacedObjectReference{
								Name:      "corporate-theme",
								Namespace: "themes-source",
							},
						},
					},
				},
				DefaultTheme:   "corporate",
				ImagePrePuller: &platformv1alpha1.ImagePrePuller{Enabled: true},
			},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		var rel *release.Release
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			rel, err = hc.Status(sessionManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		styling, ok := rel.Config["websiteStyling"].(map[string]any)
		Expect(ok).To(BeTrue(), "websiteStyling missing from rendered values")
		// The chart keys themes by Secret name; the CR-level theme name
		// translates to its backing Secret.
		Expect(styling["defaultTheme"]).To(Equal("corporate-theme"))
		refs, ok := styling["themeDataRefs"].([]any)
		Expect(ok).To(BeTrue())
		Expect(refs).To(HaveLen(1))
		Expect(refs[0]).To(Equal(map[string]any{
			"name":      "corporate-theme",
			"namespace": "themes-source",
		}))

		prePuller, ok := rel.Config["imagePrePuller"].(map[string]any)
		Expect(ok).To(BeTrue(), "imagePrePuller missing from rendered values")
		Expect(prePuller["enabled"]).To(BeTrue())
	})

	It("rejects reserved-but-unsupported spec surface with field-specific validation errors", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.SessionManagerSpec{
				DefaultAccessCredentials: &platformv1alpha1.DefaultAccessCredentials{
					Username: "educates",
				},
			},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		Eventually(func() string {
			return smgrConditionReason(conditionReady)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ValidationFailed"))

		got := &platformv1alpha1.SessionManager{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(platformv1alpha1.ComponentPhaseDegraded))
		cond := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(cond.Message).To(ContainSubstring("spec.defaultAccessCredentials"))
		Expect(cond.Message).To(ContainSubstring("not yet supported"))

		// Validation refusals must not install the chart.
		hc, err := helmFac.For(platformNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, statusErr := hc.Status(sessionManagerReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))

		// A ConfigMap-sourced theme is refused the same way after the
		// spec is corrected to drop the credentials block.
		got.Spec.DefaultAccessCredentials = nil
		got.Spec.Themes = []platformv1alpha1.Theme{
			{
				Name: "corporate",
				Source: platformv1alpha1.ThemeSource{
					Type: platformv1alpha1.ThemeSourceTypeConfigMap,
					ConfigMapRef: &platformv1alpha1.NamespacedObjectReference{
						Name:      "corporate-theme",
						Namespace: "themes-source",
					},
				},
			},
		}
		Expect(k8sClient.Update(ctx, got)).To(Succeed())

		Eventually(func() string {
			latest := &platformv1alpha1.SessionManager{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, latest); err != nil {
				return ""
			}
			cond := meta.FindStatusCondition(latest.Status.Conditions, conditionReady)
			if cond == nil {
				return ""
			}
			return cond.Message
		}, 30*time.Second, 200*time.Millisecond).Should(ContainSubstring("spec.themes[0].source.type"))
	})

	It("uninstalls the chart on delete", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
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
		markDeploymentAvailable(sessionManagerDeploymentName)
		Eventually(func() metav1.ConditionStatus {
			return smgrReadyStatus()
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

	// --- Optional extras: nodeCATrust + remoteAccess --------------

	// makeReadyClusterConfigWithCA augments the shared fixture with a
	// CA cert ref so resolveNodeCATrust(Auto/Enabled) treats the
	// prerequisite as satisfied.
	withCAOnClusterConfig := func() {
		GinkgoHelper()
		cc := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configSingletonName}, cc)).To(Succeed())
		cc.Status.Ingress.CACertificateSecretRef = &configv1alpha1.NamespacedSecretRef{
			Namespace: "educates-installer",
			Name:      "wildcard-ca",
		}
		Expect(k8sClient.Status().Update(ctx, cc)).To(Succeed())
	}

	driveSessionManagerReady := func(smgr *platformv1alpha1.SessionManager) {
		GinkgoHelper()
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(sessionManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(sessionManagerDeploymentName)
	}

	extraConditionReason := func(condType string) string {
		got := &platformv1alpha1.SessionManager{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got); err != nil {
			return ""
		}
		c := meta.FindStatusCondition(got.Status.Conditions, condType)
		if c == nil {
			return ""
		}
		return c.Reason
	}

	It("nodeCATrust Auto installs when ECC publishes a CA cert ref", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		withCAOnClusterConfig()

		driveSessionManagerReady(&platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		})

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(nodeCAInjectorReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		Eventually(func() string {
			return extraConditionReason(conditionNodeCATrustDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("Installed"))
	})

	It("nodeCATrust Auto skips when ECC has no CA cert ref", func() {
		makeReadyClusterConfig() // no CA on the fixture
		makeReadySecretsManager()

		driveSessionManagerReady(&platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		})

		Eventually(func() string {
			return extraConditionReason(conditionNodeCATrustDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ModeAutoNoCA"))

		hc, err := helmFac.For(platformNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, statusErr := hc.Status(nodeCAInjectorReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
	})

	It("nodeCATrust Enabled refuses when ECC has no CA cert ref", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		driveSessionManagerReady(&platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.SessionManagerSpec{
				NodeCATrust: &platformv1alpha1.NodeCATrust{
					Mode: platformv1alpha1.ComponentModeEnabled,
				},
			},
		})

		Eventually(func() string {
			return extraConditionReason(conditionNodeCATrustDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("NodeCATrustMissingCA"))

		// Aggregate Ready should be False with reason ExtraRefused.
		Eventually(func() metav1.ConditionStatus {
			return smgrReadyStatus()
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionFalse))
		Expect(smgrConditionReason(conditionReady)).To(Equal("ExtraRefused"))
	})

	It("remoteAccess Auto installs when a LookupService CR exists", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		// Create the LookupService CR (presence is the Auto signal —
		// readiness isn't part of the signal).
		Expect(k8sClient.Create(ctx, &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		})).To(Succeed())

		driveSessionManagerReady(&platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		})

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(remoteAccessReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		Eventually(func() string {
			return extraConditionReason(conditionRemoteAccessDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("Installed"))
	})

	It("remoteAccess Auto skips when no LookupService CR exists", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		driveSessionManagerReady(&platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		})

		Eventually(func() string {
			return extraConditionReason(conditionRemoteAccessDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ModeAutoNoLookupService"))
	})

	It("Disabled drains a previously-installed extra", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		withCAOnClusterConfig()

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		}
		driveSessionManagerReady(smgr)

		// Auto + CA → installed.
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(nodeCAInjectorReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		// Flip to Disabled — reconciler should drain.
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, smgr)).To(Succeed())
		smgr.Spec.NodeCATrust = &platformv1alpha1.NodeCATrust{
			Mode: platformv1alpha1.ComponentModeDisabled,
		}
		Expect(k8sClient.Update(ctx, smgr)).To(Succeed())

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, statusErr := hc.Status(nodeCAInjectorReleaseName)
			return statusErr
		}, 30*time.Second, 200*time.Millisecond).Should(MatchError(helm.ErrReleaseNotFound))
		Eventually(func() string {
			return extraConditionReason(conditionNodeCATrustDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ModeDisabled"))
	})
})
