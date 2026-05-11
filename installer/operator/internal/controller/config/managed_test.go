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
	"sync"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// validManagedSpec returns a minimal Managed-mode spec that satisfies
// Phase 2 Session 2 validation: BundledContour + BundledCertManager
// with a CustomCA issuer. Used by every spec in this file as the
// shared happy-path starting point.
func validManagedSpec() configv1alpha1.EducatesClusterConfigSpec {
	return configv1alpha1.EducatesClusterConfigSpec{
		Mode: configv1alpha1.ClusterConfigModeManaged,
		Ingress: &configv1alpha1.Ingress{
			Domain:           "educates.test",
			IngressClassName: "contour",
			Controller: configv1alpha1.IngressController{
				Provider: configv1alpha1.IngressControllerProviderBundledContour,
			},
			Certificates: configv1alpha1.Certificates{
				Provider: configv1alpha1.CertificatesProviderBundledCertManager,
				BundledCertManager: &configv1alpha1.BundledCertManagerConfig{
					IssuerType: configv1alpha1.IssuerTypeCustomCA,
					CustomCA: &configv1alpha1.CustomCAConfig{
						CACertificateRef: configv1alpha1.LocalObjectReference{
							Name: "custom-ca",
						},
					},
				},
			},
		},
	}
}

// makeCustomCASecret returns a tls.crt + tls.key Secret in the operator
// namespace. checkCustomCASecret only verifies key presence, so byte
// values are irrelevant — the validator never parses them.
func makeCustomCASecret(name string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testOperatorNamespace},
		Data: map[string][]byte{
			"tls.crt": []byte("dummy-ca-cert"),
			"tls.key": []byte("dummy-ca-key"),
		},
	}
}

// memoryHelmFactory builds an in-memory Helm client per namespace and
// memoises the result. Returning a stable client per namespace lets
// tests assert against the release store the reconciler writes to.
type memoryHelmFactory struct {
	mu      sync.Mutex
	clients map[string]*helm.Client
}

func newMemoryHelmFactory() *memoryHelmFactory {
	return &memoryHelmFactory{clients: map[string]*helm.Client{}}
}

func (f *memoryHelmFactory) For(ns string) (*helm.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[ns]; ok {
		return c, nil
	}
	c, err := helm.NewMemoryClient(ns)
	if err != nil {
		return nil, err
	}
	f.clients[ns] = c
	return c, nil
}

var _ = Describe("EducatesClusterConfig Managed-mode reconciler (Phase 2 Session 2)", func() {
	var (
		mgrCancel context.CancelFunc
		mgrDone   chan error
		helmFac   *memoryHelmFactory
	)

	BeforeEach(func() {
		ensureNamespace(testOperatorNamespace)
		helmFac = newMemoryHelmFactory()

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
			Metrics:    metricsserver.Options{BindAddress: "0"},
			Controller: crconfig.Controller{SkipNameValidation: ptr.To(true)},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect((&EducatesClusterConfigReconciler{
			Client:            mgr.GetClient(),
			Scheme:            mgr.GetScheme(),
			OperatorNamespace: testOperatorNamespace,
			HelmClientFor:     helmFac.For,
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
		_ = k8sClient.DeleteAllOf(ctx, &cmv1.ClusterIssuer{})
		// cert-manager namespace is cluster-scoped; clean up so the next
		// spec starts from a known state. ignore not-found.
		_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: certManagerNamespace}})
	})

	It("installs cert-manager from the embedded chart and records its version", func() {
		Expect(k8sClient.Create(ctx, makeCustomCASecret("custom-ca"))).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validManagedSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		// status.bundledChartVersions[cert-manager] populated after install.
		Eventually(func() string {
			got := &configv1alpha1.EducatesClusterConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
				return ""
			}
			return got.Status.BundledChartVersions["cert-manager"]
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(vendoredcharts.CertManagerVersion))

		// cert-manager namespace is created with the operator's managed-by
		// label and owned by the EducatesClusterConfig.
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: certManagerNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		ns := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: certManagerNamespace}, ns)).To(Succeed())
		Expect(ns.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", managedByLabelValue))
		Expect(ns.OwnerReferences).To(HaveLen(1))
		Expect(ns.OwnerReferences[0].Kind).To(Equal("EducatesClusterConfig"))

		// The in-memory Helm store holds a cert-manager release.
		hc, err := helmFac.For(certManagerNamespace)
		Expect(err).NotTo(HaveOccurred())
		rel, err := hc.Status(certManagerReleaseName)
		Expect(err).NotTo(HaveOccurred())
		Expect(rel.Chart.Metadata.Name).To(Equal("cert-manager"))

		// CertificatesReady is False/Installing pending Session 2 commit 2.
		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		cond := meta.FindStatusCondition(got.Status.Conditions, conditionCertificatesReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("Installing"))
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseInstalling))
	})

	It("flips to Degraded when the CustomCA Secret is missing", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validManagedSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionFalse))

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))

		// Helm release must NOT exist — validator failed before install.
		hc, err := helmFac.For(certManagerNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, statusErr := hc.Status(certManagerReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
	})

	It("rejects not-yet-supported providers with explicit validation errors", func() {
		spec := validManagedSpec()
		spec.Ingress.Certificates.Provider = configv1alpha1.CertificatesProviderStaticCertificate
		spec.Ingress.Certificates.BundledCertManager = nil
		spec.Ingress.Certificates.StaticCertificate = &configv1alpha1.StaticCertificateConfig{
			TLSSecretRef: configv1alpha1.LocalObjectReference{Name: "wildcard-tls"},
		}

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		Eventually(func() string {
			got := &configv1alpha1.EducatesClusterConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
				return ""
			}
			cond := meta.FindStatusCondition(got.Status.Conditions, conditionValidationSucceeded)
			if cond == nil {
				return ""
			}
			return cond.Message
		}, 30*time.Second, 200*time.Millisecond).Should(ContainSubstring("not yet supported"))
	})
})

