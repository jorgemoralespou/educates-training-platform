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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkshopPolicyEngine names the engine enforcing per-workshop isolation
// rules. Mirrors the same-named enum in the config API group;
// duplicated to avoid cross-group Go coupling.
// +kubebuilder:validation:Enum=Kyverno;None
type WorkshopPolicyEngine string

const (
	WorkshopPolicyEngineKyverno WorkshopPolicyEngine = "Kyverno"
	WorkshopPolicyEngineNone    WorkshopPolicyEngine = "None"
)

// ThemeSourceType selects how a theme's content is sourced.
// Additional types may be added by the session-manager owner.
// +kubebuilder:validation:Enum=ConfigMap;Secret;URL
type ThemeSourceType string

const (
	ThemeSourceTypeConfigMap ThemeSourceType = "ConfigMap"
	ThemeSourceTypeSecret    ThemeSourceType = "Secret"
	ThemeSourceTypeURL       ThemeSourceType = "URL"
)

// IngressOverrides allows SessionManager to override the cluster-wide
// ingress secrets for the bare-domain hostnames it serves directly
// (TrainingPortal CRs prefix the domain for individual portals).
type IngressOverrides struct {
	// +optional
	TLSSecretRef *LocalObjectReference `json:"tlsSecretRef,omitempty"`

	// +optional
	CACertificateSecretRef *LocalObjectReference `json:"caCertificateSecretRef,omitempty"`
}

// WorkshopPolicyOverride locally overrides
// EducatesClusterConfig.status.policyEnforcement.workshopPolicyEngine
// for this SessionManager.
type WorkshopPolicyOverride struct {
	// +required
	Engine WorkshopPolicyEngine `json:"engine"`
}

// ImageOverride entries replace one chart-default image by short name.
// Mirrors the v3 imageVersions shape: any image the chart's default
// inventory exposes by name can be overridden here.
type ImageOverride struct {
	// name matches an entry in the chart's image-versions inventory
	// (e.g., "session-manager", "training-portal", "jdk17-environment").
	// +required
	Name string `json:"name"`

	// image is the full reference including tag or digest.
	// +required
	Image string `json:"image"`
}

// Images groups image-related overrides. Registry prefix and pull
// secrets are inherited from
// EducatesClusterConfig.status.imageRegistry; only per-image overrides
// belong here.
type Images struct {
	// +optional
	Overrides []ImageOverride `json:"overrides,omitempty"`
}

// NamespacedObjectReference references an object by name and namespace.
type NamespacedObjectReference struct {
	// +required
	Name string `json:"name"`

	// +required
	Namespace string `json:"namespace"`
}

// ThemeSource sources theme content. Exactly one of the per-type fields
// (configMapRef, etc.) should be populated for the selected type.
type ThemeSource struct {
	// +required
	Type ThemeSourceType `json:"type"`

	// configMapRef applies when type is ConfigMap.
	// +optional
	ConfigMapRef *NamespacedObjectReference `json:"configMapRef,omitempty"`
}

// Theme is one named entry in the spec.themes list.
type Theme struct {
	// +required
	Name string `json:"name"`

	// +required
	Source ThemeSource `json:"source"`
}

// TrackingProvider holds a single analytics provider's tracking ID.
type TrackingProvider struct {
	// +required
	TrackingID string `json:"trackingId"`
}

// TrackingWebhook configures an HTTP webhook receiver for analytics
// events.
type TrackingWebhook struct {
	// +required
	URL string `json:"url"`
}

// Tracking groups analytics provider configuration.
type Tracking struct {
	// +optional
	GoogleAnalytics *TrackingProvider `json:"googleAnalytics,omitempty"`

	// +optional
	Amplitude *TrackingProvider `json:"amplitude,omitempty"`

	// +optional
	Clarity *TrackingProvider `json:"clarity,omitempty"`

	// +optional
	Webhook *TrackingWebhook `json:"webhook,omitempty"`
}

// DefaultAccessCredentials configures the default
// username/password used for workshop access when a TrainingPortal
// doesn't override them.
type DefaultAccessCredentials struct {
	// +optional
	Username string `json:"username,omitempty"`

	// passwordSecretRef references a Secret holding the password value.
	// +optional
	PasswordSecretRef *LocalObjectReference `json:"passwordSecretRef,omitempty"`
}

// SessionStorage configures persistent storage characteristics for
// workshop sessions.
type SessionStorage struct {
	// +optional
	StorageClass string `json:"storageClass,omitempty"`

	// storageGroup sets the supplemental GID for mounted volumes.
	// +optional
	StorageGroup *int64 `json:"storageGroup,omitempty"`

	// storageUser sets the UID for mounted volumes.
	// +optional
	StorageUser *int64 `json:"storageUser,omitempty"`
}

// SessionNetwork configures network characteristics applied to workshop
// sessions.
type SessionNetwork struct {
	// packetSize sets the MTU for workshop session networking. Useful
	// on overlay networks where the default MTU is too large.
	// +kubebuilder:validation:Minimum=576
	// +optional
	PacketSize *int32 `json:"packetSize,omitempty"`

	// blockedCidrs lists CIDR ranges workshop sessions are denied
	// network access to (e.g., cloud metadata endpoints).
	// +optional
	BlockedCIDRs []string `json:"blockedCidrs,omitempty"`
}

// ImageCache configures the optional in-cluster image cache used to
// accelerate workshop image pulls.
type ImageCache struct {
	// +kubebuilder:default=false
	// +optional
	Enabled bool `json:"enabled,omitempty"`
}

// RegistryMirror declares a registry mirror used by workshop containers.
type RegistryMirror struct {
	// mirror is the upstream registry being mirrored
	// (e.g., "docker.io").
	// +required
	Mirror string `json:"mirror"`

	// url is the mirror endpoint.
	// +required
	URL string `json:"url"`
}

// SessionManagerSpec defines the desired state of SessionManager.
//
// Requires SecretsManager.Ready and EducatesClusterConfig.Ready; both
// dependencies are singletons so no explicit refs are carried.
//
// Image registry prefix and pull secrets are inherited from
// EducatesClusterConfig.status.imageRegistry; only per-image overrides
// land in spec.images.overrides.
type SessionManagerSpec struct {
	// +optional
	IngressOverrides *IngressOverrides `json:"ingressOverrides,omitempty"`

	// +optional
	WorkshopPolicyOverride *WorkshopPolicyOverride `json:"workshopPolicyOverride,omitempty"`

	// +optional
	Images *Images `json:"images,omitempty"`

	// themes is a list of named themes available to TrainingPortals.
	// +optional
	Themes []Theme `json:"themes,omitempty"`

	// defaultTheme names the entry from themes used as the install-wide
	// default. Must match a Theme.name.
	// +optional
	DefaultTheme string `json:"defaultTheme,omitempty"`

	// +optional
	Tracking *Tracking `json:"tracking,omitempty"`

	// +optional
	DefaultAccessCredentials *DefaultAccessCredentials `json:"defaultAccessCredentials,omitempty"`

	// sessionCookieDomain sets the cookie domain used by workshop
	// sessions for cross-subdomain authentication.
	// +optional
	SessionCookieDomain string `json:"sessionCookieDomain,omitempty"`

	// allowedEmbeddingHosts lists hosts allowed to embed Educates
	// workshop frames (CSP frame-ancestors).
	// +optional
	AllowedEmbeddingHosts []string `json:"allowedEmbeddingHosts,omitempty"`

	// +optional
	Storage *SessionStorage `json:"storage,omitempty"`

	// +optional
	Network *SessionNetwork `json:"network,omitempty"`

	// +optional
	ImageCache *ImageCache `json:"imageCache,omitempty"`

	// registryMirrors configures per-registry mirrors for workshop
	// container pulls.
	// +optional
	RegistryMirrors []RegistryMirror `json:"registryMirrors,omitempty"`

	// logLevel defaults to info.
	// +kubebuilder:default=info
	// +optional
	LogLevel LogLevel `json:"logLevel,omitempty"`
}

// SessionManagerStatus defines the observed state of SessionManager.
// Phase 4 publishes the full CRD draft r3 §4 contract: phase +
// conditions + installedVersion + deploymentRef.
type SessionManagerStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	Phase ComponentPhase `json:"phase,omitempty"`

	// conditions report the resource's state. Phase 4 publishes:
	//   - Ready                    (aggregate)
	//   - ClusterConfigAvailable   (EducatesClusterConfig.Ready gate)
	//   - SecretsManagerAvailable  (SecretsManager.Ready gate)
	//   - Deployed                 (helm release + Deployment Available)
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// installedVersion records the session-manager chart version most
	// recently applied.
	// +optional
	InstalledVersion string `json:"installedVersion,omitempty"`

	// deploymentRef names the upstream session-manager Deployment the
	// operator is gating Ready on.
	// +optional
	DeploymentRef *NamespacedRef `json:"deploymentRef,omitempty"`
}

// SessionManager is the singleton resource that drives installation of
// the session-manager component (with training-portal,
// assets-server, image-cache, and supporting services).
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="SessionManager must be named 'cluster' (singleton per cluster)"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type SessionManager struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SessionManagerSpec `json:"spec"`

	// +optional
	Status SessionManagerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SessionManagerList contains a list of SessionManager.
type SessionManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SessionManager `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SessionManager{}, &SessionManagerList{})
}
