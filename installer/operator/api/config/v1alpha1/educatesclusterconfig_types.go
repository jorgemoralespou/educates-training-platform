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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterConfigMode selects between operator-managed and user-declared
// cluster infrastructure. Immutable once set; switching modes requires
// deleting and recreating the resource.
// +kubebuilder:validation:Enum=Managed;Inline
type ClusterConfigMode string

const (
	// ClusterConfigModeManaged: operator installs and reconciles cluster
	// services (cert-manager, contour, kyverno, external-dns) per spec.
	ClusterConfigModeManaged ClusterConfigMode = "Managed"
	// ClusterConfigModeInline: operator validates user-declared
	// pre-existing resources and publishes them in status; installs
	// nothing.
	ClusterConfigModeInline ClusterConfigMode = "Inline"
)

// InfrastructureProvider identifies the underlying cluster substrate.
// Used by the operator to compute provider-specific defaults and to
// validate cloud-related fields.
// +kubebuilder:validation:Enum=Kind;Minikube;EKS;GKE;OpenShift;VCluster;Generic
type InfrastructureProvider string

const (
	InfrastructureProviderKind      InfrastructureProvider = "Kind"
	InfrastructureProviderMinikube  InfrastructureProvider = "Minikube"
	InfrastructureProviderEKS       InfrastructureProvider = "EKS"
	InfrastructureProviderGKE       InfrastructureProvider = "GKE"
	InfrastructureProviderOpenShift InfrastructureProvider = "OpenShift"
	InfrastructureProviderVCluster  InfrastructureProvider = "VCluster"
	InfrastructureProviderGeneric   InfrastructureProvider = "Generic"
)

// IngressControllerProvider selects how the cluster's ingress controller
// is provided.
// +kubebuilder:validation:Enum=BundledContour;ExternalIngressController
type IngressControllerProvider string

const (
	IngressControllerProviderBundledContour            IngressControllerProvider = "BundledContour"
	IngressControllerProviderExternalIngressController IngressControllerProvider = "ExternalIngressController"
)

// CertificatesProvider selects how the wildcard TLS certificate is
// provisioned.
// +kubebuilder:validation:Enum=BundledCertManager;ExternalCertManager;StaticCertificate
type CertificatesProvider string

const (
	CertificatesProviderBundledCertManager  CertificatesProvider = "BundledCertManager"
	CertificatesProviderExternalCertManager CertificatesProvider = "ExternalCertManager"
	CertificatesProviderStaticCertificate   CertificatesProvider = "StaticCertificate"
)

// IssuerType selects the cert-manager ClusterIssuer flavour for the
// BundledCertManager provider.
// +kubebuilder:validation:Enum=ACME;CustomCA
type IssuerType string

const (
	IssuerTypeACME     IssuerType = "ACME"
	IssuerTypeCustomCA IssuerType = "CustomCA"
)

// DNS01Provider names a cert-manager DNS01 solver. Required for wildcard
// certificate issuance via ACME.
// +kubebuilder:validation:Enum=Route53;CloudDNS;Cloudflare;AzureDNS
type DNS01Provider string

const (
	DNS01ProviderRoute53    DNS01Provider = "Route53"
	DNS01ProviderCloudDNS   DNS01Provider = "CloudDNS"
	DNS01ProviderCloudflare DNS01Provider = "Cloudflare"
	DNS01ProviderAzureDNS   DNS01Provider = "AzureDNS"
)

// DNSProvider selects how DNS records are managed.
// +kubebuilder:validation:Enum=BundledExternalDNS;Manual;None
type DNSProvider string

const (
	DNSProviderBundledExternalDNS DNSProvider = "BundledExternalDNS"
	DNSProviderManual             DNSProvider = "Manual"
	DNSProviderNone               DNSProvider = "None"
)

// ClusterPolicyEngine names the cluster-wide policy enforcement engine.
// +kubebuilder:validation:Enum=Kyverno;PodSecurityStandards;OpenShiftSCC;None
type ClusterPolicyEngine string

const (
	ClusterPolicyEngineKyverno              ClusterPolicyEngine = "Kyverno"
	ClusterPolicyEnginePodSecurityStandards ClusterPolicyEngine = "PodSecurityStandards"
	ClusterPolicyEngineOpenShiftSCC         ClusterPolicyEngine = "OpenShiftSCC"
	ClusterPolicyEngineNone                 ClusterPolicyEngine = "None"
)

// WorkshopPolicyEngine names the engine enforcing per-workshop isolation
// rules. Setting to None disables workshop isolation.
// +kubebuilder:validation:Enum=Kyverno;None
type WorkshopPolicyEngine string

const (
	WorkshopPolicyEngineKyverno WorkshopPolicyEngine = "Kyverno"
	WorkshopPolicyEngineNone    WorkshopPolicyEngine = "None"
)

// KyvernoProvider selects whether Kyverno is operator-installed or
// pre-existing.
// +kubebuilder:validation:Enum=Bundled;External
type KyvernoProvider string

const (
	KyvernoProviderBundled  KyvernoProvider = "Bundled"
	KyvernoProviderExternal KyvernoProvider = "External"
)

// ClusterConfigPhase summarises the operator's current activity on this
// resource. Phases are advisory; conditions carry the authoritative
// state.
// +kubebuilder:validation:Enum=Pending;Installing;Validating;Ready;Degraded;Uninstalling
type ClusterConfigPhase string

const (
	ClusterConfigPhasePending      ClusterConfigPhase = "Pending"
	ClusterConfigPhaseInstalling   ClusterConfigPhase = "Installing"
	ClusterConfigPhaseValidating   ClusterConfigPhase = "Validating"
	ClusterConfigPhaseReady        ClusterConfigPhase = "Ready"
	ClusterConfigPhaseDegraded     ClusterConfigPhase = "Degraded"
	ClusterConfigPhaseUninstalling ClusterConfigPhase = "Uninstalling"
)

// LocalObjectReference is a reference to a Kubernetes object by name in
// the operator namespace. Cluster-scoped references (e.g., ClusterIssuer,
// IngressClass) also use this shape.
type LocalObjectReference struct {
	// name of the referent.
	// +required
	Name string `json:"name"`
}

// SecretKeyRef references a key within a Secret in the operator namespace.
type SecretKeyRef struct {
	// name of the Secret.
	// +required
	Name string `json:"name"`

	// key within the Secret. Defaults vary by use site.
	// +optional
	Key string `json:"key,omitempty"`
}

// OperationalBlock collects the per-Deployment operational knobs that
// every Bundled cluster-service block exposes. Per the r3 design the
// shape is duplicated at each use site rather than abstracted, leaving
// room for deployment-specific variants in future revisions.
type OperationalBlock struct {
	// replicas overrides the operator-computed default. The default
	// varies by infrastructure provider (typically 1 for Kind/Minikube,
	// 2+ otherwise).
	// +kubebuilder:validation:Minimum=0
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`

	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`
}

// CloudConfig groups cloud-provider-specific configuration. The
// service-account fields hold opaque identity strings interpreted by the
// provider (e.g., GCP service-account email for GKE, IAM role ARN for
// EKS).
type CloudConfig struct {
	// project / account identifier, e.g., GCP project ID or AWS account
	// alias.
	// +optional
	Project string `json:"project,omitempty"`

	// +optional
	Region string `json:"region,omitempty"`

	// +optional
	ServiceAccounts *CloudServiceAccounts `json:"serviceAccounts,omitempty"`
}

// CloudServiceAccounts maps Educates' bundled cluster services to
// provider-native workload identities.
type CloudServiceAccounts struct {
	// certManager identity used by cert-manager when requesting
	// DNS01-validated certificates.
	// +optional
	CertManager string `json:"certManager,omitempty"`

	// externalDNS identity used by external-dns when managing DNS
	// records.
	// +optional
	ExternalDNS string `json:"externalDNS,omitempty"`
}

// Infrastructure describes the cluster substrate on which Educates
// runs. Required when mode is Managed; ignored in Inline mode.
type Infrastructure struct {
	// +required
	Provider InfrastructureProvider `json:"provider"`

	// cloud carries provider-specific configuration. Required for cloud
	// providers (EKS, GKE) when bundled cert-manager or external-dns is
	// enabled.
	// +optional
	Cloud *CloudConfig `json:"cloud,omitempty"`
}

// Route53Config configures the Route53 DNS01 solver.
type Route53Config struct {
	// +required
	HostedZoneID string `json:"hostedZoneID"`

	// region defaults to spec.infrastructure.cloud.region when unset.
	// +optional
	Region string `json:"region,omitempty"`
}

// CloudDNSConfig configures the GCP CloudDNS DNS01 solver.
type CloudDNSConfig struct {
	// +required
	Zone string `json:"zone"`

	// project defaults to spec.infrastructure.cloud.project when unset.
	// +optional
	Project string `json:"project,omitempty"`
}

// CloudflareConfig configures the Cloudflare DNS01 solver.
type CloudflareConfig struct {
	// apiTokenSecretRef references a Secret holding the Cloudflare API
	// token. The default key is "api-token".
	// +required
	APITokenSecretRef SecretKeyRef `json:"apiTokenSecretRef"`
}

// AzureDNSConfig configures the Azure DNS DNS01 solver.
type AzureDNSConfig struct {
	// +required
	ResourceGroup string `json:"resourceGroup"`

	// +required
	SubscriptionID string `json:"subscriptionID"`
}

// ACMEDNS01Solver selects and configures a DNS01 solver. DNS01 is
// required for wildcard certificate issuance.
type ACMEDNS01Solver struct {
	// +required
	Provider DNS01Provider `json:"provider"`

	// +optional
	Route53 *Route53Config `json:"route53,omitempty"`

	// +optional
	CloudDNS *CloudDNSConfig `json:"cloudDNS,omitempty"`

	// +optional
	Cloudflare *CloudflareConfig `json:"cloudflare,omitempty"`

	// +optional
	AzureDNS *AzureDNSConfig `json:"azureDNS,omitempty"`
}

// ACMEHTTP01Solver configures the optional HTTP01 solver. Rarely needed
// because DNS01 is required for wildcards.
type ACMEHTTP01Solver struct {
	// ingressClassName defaults to spec.ingress.ingressClassName when
	// unset.
	// +optional
	IngressClassName string `json:"ingressClassName,omitempty"`
}

// ACMESolvers groups the cert-manager solvers used to satisfy the ACME
// challenge.
type ACMESolvers struct {
	// dns01 is required for wildcard issuance.
	// +required
	DNS01 ACMEDNS01Solver `json:"dns01"`

	// +optional
	HTTP01 *ACMEHTTP01Solver `json:"http01,omitempty"`
}

// ACMEConfig configures the cert-manager ACME ClusterIssuer.
type ACMEConfig struct {
	// +required
	Email string `json:"email"`

	// +required
	Solvers ACMESolvers `json:"solvers"`
}

// CustomCAConfig configures a self-signed/custom CA-backed ClusterIssuer.
type CustomCAConfig struct {
	// caCertificateRef references a Secret holding the CA's own cert and
	// key (keys: tls.crt, tls.key).
	// +required
	CACertificateRef LocalObjectReference `json:"caCertificateRef"`
}

// BundledCertManagerConfig configures the operator-installed cert-manager
// chart and the ClusterIssuer it provides.
type BundledCertManagerConfig struct {
	// +required
	IssuerType IssuerType `json:"issuerType"`

	// +optional
	ACME *ACMEConfig `json:"acme,omitempty"`

	// +optional
	CustomCA *CustomCAConfig `json:"customCA,omitempty"`

	// operational tunes the cert-manager controller Deployment.
	// +optional
	Operational *OperationalBlock `json:"operational,omitempty"`
}

// ExternalCertManagerConfig assumes cert-manager is already installed
// and references an existing ClusterIssuer; the operator only creates
// the wildcard Certificate.
type ExternalCertManagerConfig struct {
	// +required
	ClusterIssuerRef LocalObjectReference `json:"clusterIssuerRef"`
}

// StaticCertificateConfig declares a pre-provisioned wildcard TLS
// certificate; no cert-manager is involved.
type StaticCertificateConfig struct {
	// tlsSecretRef references a kubernetes.io/tls Secret with keys
	// tls.crt and tls.key.
	// +required
	TLSSecretRef LocalObjectReference `json:"tlsSecretRef"`

	// caCertificateRef optionally references a Secret with the ca.crt
	// key for the issuing CA chain.
	// +optional
	CACertificateRef *LocalObjectReference `json:"caCertificateRef,omitempty"`
}

// Certificates groups certificate-provider configuration.
type Certificates struct {
	// +required
	Provider CertificatesProvider `json:"provider"`

	// +optional
	BundledCertManager *BundledCertManagerConfig `json:"bundledCertManager,omitempty"`

	// +optional
	ExternalCertManager *ExternalCertManagerConfig `json:"externalCertManager,omitempty"`

	// +optional
	StaticCertificate *StaticCertificateConfig `json:"staticCertificate,omitempty"`
}

// BundledContourConfig configures the operator-installed Contour ingress
// controller.
type BundledContourConfig struct {
	// +optional
	Operational *OperationalBlock `json:"operational,omitempty"`
}

// IngressController groups ingress-controller configuration.
type IngressController struct {
	// +required
	Provider IngressControllerProvider `json:"provider"`

	// +optional
	BundledContour *BundledContourConfig `json:"bundledContour,omitempty"`
}

// Ingress groups ingress-related configuration. Required when mode is
// Managed.
type Ingress struct {
	// domain is the wildcard subdomain under which Educates serves
	// workshops, e.g., "educates.example.com".
	// +required
	Domain string `json:"domain"`

	// ingressClassName names the IngressClass used by Educates. In
	// BundledContour mode the operator creates an IngressClass with
	// this name; in External mode it must already exist.
	// +required
	IngressClassName string `json:"ingressClassName"`

	// +required
	Controller IngressController `json:"controller"`

	// +required
	Certificates Certificates `json:"certificates"`
}

// BundledExternalDNSConfig configures the operator-installed external-dns
// chart. Zone discovery is automatic from Ingress hostnames; explicit
// zones may be added in a later revision.
type BundledExternalDNSConfig struct {
	// +optional
	Operational *OperationalBlock `json:"operational,omitempty"`
}

// DNS groups DNS-management configuration.
type DNS struct {
	// provider defaults to None — appropriate for local clusters using
	// nip.io or hosts-file resolution. Cloud installs must set this
	// explicitly.
	// +kubebuilder:default=None
	// +optional
	Provider DNSProvider `json:"provider,omitempty"`

	// +optional
	BundledExternalDNS *BundledExternalDNSConfig `json:"bundledExternalDNS,omitempty"`
}

// ClusterPolicyConfig configures the cluster-wide policy engine.
type ClusterPolicyConfig struct {
	// engine defaults to Kyverno.
	// +kubebuilder:default=Kyverno
	// +optional
	Engine ClusterPolicyEngine `json:"engine,omitempty"`
}

// WorkshopPolicyConfig configures the per-workshop isolation engine.
type WorkshopPolicyConfig struct {
	// engine defaults to Kyverno. Setting to None disables workshop
	// isolation; the cluster operator takes responsibility for
	// containment.
	// +kubebuilder:default=Kyverno
	// +optional
	Engine WorkshopPolicyEngine `json:"engine,omitempty"`
}

// BundledKyvernoConfig configures the operator-installed Kyverno chart.
type BundledKyvernoConfig struct {
	// +optional
	Operational *OperationalBlock `json:"operational,omitempty"`
}

// KyvernoConfig groups Kyverno-engine sourcing. Required when any
// policyEnforcement engine resolves to Kyverno.
type KyvernoConfig struct {
	// provider defaults to Bundled.
	// +kubebuilder:default=Bundled
	// +optional
	Provider KyvernoProvider `json:"provider,omitempty"`

	// +optional
	Bundled *BundledKyvernoConfig `json:"bundled,omitempty"`
}

// PolicyEnforcement groups cluster-wide and per-workshop policy
// configuration.
type PolicyEnforcement struct {
	// +required
	ClusterPolicy ClusterPolicyConfig `json:"clusterPolicy"`

	// +required
	WorkshopPolicy WorkshopPolicyConfig `json:"workshopPolicy"`

	// kyverno is required when either engine above resolves to Kyverno.
	// +optional
	Kyverno *KyvernoConfig `json:"kyverno,omitempty"`
}

// ImageRegistry configures registry rewriting and pull credentials.
// Applies to all bundled charts in Managed mode and to the runtime in
// both modes.
type ImageRegistry struct {
	// prefix rewrites every bundled image reference to live under this
	// prefix, e.g., "internal-registry.corp.local/educates". Pre-relocated
	// bundles (via helm dt wrap/unwrap) do not need this set.
	// +optional
	Prefix string `json:"prefix,omitempty"`

	// pullSecrets references kubernetes.io/dockerconfigjson Secrets in
	// the operator namespace.
	// +optional
	PullSecrets []LocalObjectReference `json:"pullSecrets,omitempty"`
}

// InlineIngress declares pre-existing ingress resources for Inline
// mode. The operator validates these and republishes them in status.
type InlineIngress struct {
	// +required
	Domain string `json:"domain"`

	// +required
	IngressClassName string `json:"ingressClassName"`

	// wildcardCertificateSecretRef references a kubernetes.io/tls Secret
	// with keys tls.crt and tls.key, valid for *.<domain>.
	// +required
	WildcardCertificateSecretRef LocalObjectReference `json:"wildcardCertificateSecretRef"`

	// caCertificateSecretRef references a Secret with the ca.crt key
	// for the issuing CA chain. Optional.
	// +optional
	CACertificateSecretRef *LocalObjectReference `json:"caCertificateSecretRef,omitempty"`

	// clusterIssuerRef references an existing ClusterIssuer that must be
	// Ready. Optional; informational for components.
	// +optional
	ClusterIssuerRef *LocalObjectReference `json:"clusterIssuerRef,omitempty"`
}

// InlinePolicyEnforcement declares the policy engines already in place
// for Inline mode. Enforced engines are identified, not installed.
type InlinePolicyEnforcement struct {
	// +required
	ClusterPolicyEngine ClusterPolicyEngine `json:"clusterPolicyEngine"`

	// +required
	WorkshopPolicyEngine WorkshopPolicyEngine `json:"workshopPolicyEngine"`
}

// InlineConfig groups all Inline-mode user assertions about pre-existing
// cluster state.
type InlineConfig struct {
	// +required
	Ingress InlineIngress `json:"ingress"`

	// +required
	PolicyEnforcement InlinePolicyEnforcement `json:"policyEnforcement"`

	// +optional
	ImageRegistry *ImageRegistry `json:"imageRegistry,omitempty"`
}

// EducatesClusterConfigSpec defines the desired state of
// EducatesClusterConfig. Mode-field exclusivity (Managed fields forbidden
// when mode is Inline, and vice versa) is reconciler-validated in
// Phase 0; a structural CEL rule is added in Phase 1.
//
// +kubebuilder:validation:XValidation:rule="self.mode == oldSelf.mode",message="spec.mode is immutable; delete and recreate the resource to switch modes"
type EducatesClusterConfigSpec struct {
	// +required
	Mode ClusterConfigMode `json:"mode"`

	// infrastructure describes the cluster substrate. Used in Managed
	// mode; ignored in Inline mode.
	// +optional
	Infrastructure *Infrastructure `json:"infrastructure,omitempty"`

	// ingress configures the Educates ingress in Managed mode; ignored
	// in Inline mode.
	// +optional
	Ingress *Ingress `json:"ingress,omitempty"`

	// dns configures DNS management in Managed mode; ignored in Inline
	// mode.
	// +optional
	DNS *DNS `json:"dns,omitempty"`

	// policyEnforcement configures the cluster and workshop policy
	// engines in Managed mode; ignored in Inline mode.
	// +optional
	PolicyEnforcement *PolicyEnforcement `json:"policyEnforcement,omitempty"`

	// imageRegistry rewrites bundled chart image refs and supplies pull
	// credentials. Applies in Managed mode (Inline mode has its own
	// equivalent under spec.inline.imageRegistry).
	// +optional
	ImageRegistry *ImageRegistry `json:"imageRegistry,omitempty"`

	// inline declares pre-existing cluster resources. Used in Inline
	// mode; ignored in Managed mode.
	// +optional
	Inline *InlineConfig `json:"inline,omitempty"`
}

// EducatesClusterConfigStatus is the public interface that component CRs
// (SecretsManager, LookupService, SessionManager) consume. Phase 0
// publishes only the minimum surface required to drive controller-level
// state; richer fields (ingress, policyEnforcement, imageRegistry,
// bundledChartVersions) are added alongside the reconcilers that produce
// them.
type EducatesClusterConfigStatus struct {
	// observedGeneration tracks the spec generation last reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// phase is an advisory summary of the operator's current activity on
	// this resource; conditions carry the authoritative state.
	// +optional
	Phase ClusterConfigPhase `json:"phase,omitempty"`

	// conditions report the resource's state. Standard type "Ready"
	// reflects overall readiness; phase-specific types
	// (ValidationSucceeded, IngressReady, CertificatesReady, ...) are
	// added with their producing reconcilers.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// EducatesClusterConfig is the singleton resource describing the
// cluster-wide configuration of an Educates installation.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=ecc
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="EducatesClusterConfig must be named 'cluster' (singleton per cluster)"
// +kubebuilder:printcolumn:name="Mode",type="string",JSONPath=".spec.mode"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type EducatesClusterConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec EducatesClusterConfigSpec `json:"spec"`

	// +optional
	Status EducatesClusterConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// EducatesClusterConfigList contains a list of EducatesClusterConfig.
type EducatesClusterConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []EducatesClusterConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EducatesClusterConfig{}, &EducatesClusterConfigList{})
}
