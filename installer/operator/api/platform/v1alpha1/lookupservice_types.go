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

// LookupServiceIngress configures the lookup-service Ingress.
type LookupServiceIngress struct {
	// prefix combines with EducatesClusterConfig.status.ingress.domain
	// to form the full hostname (e.g., "educates-api" with domain
	// "educates.example.com" yields "educates-api.educates.example.com").
	// +required
	Prefix string `json:"prefix"`

	// tlsSecretRef optionally overrides the cluster wildcard
	// certificate. When unset, the ingress uses
	// EducatesClusterConfig.status.ingress.wildcardCertificateSecretRef.
	// +optional
	TLSSecretRef *LocalObjectReference `json:"tlsSecretRef,omitempty"`
}

// LookupServiceSpec defines the desired state of LookupService.
//
// Component-specific settings (auth, rate-limiting, storage) will be
// added when the lookup-service owner specifies them; intentionally
// out-of-scope for the v1alpha1 surface.
type LookupServiceSpec struct {
	// +required
	Ingress LookupServiceIngress `json:"ingress"`

	// +optional
	Image *ImageRef `json:"image,omitempty"`

	// logLevel defaults to info.
	// +kubebuilder:default=info
	// +optional
	LogLevel LogLevel `json:"logLevel,omitempty"`

	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// LookupServiceStatus defines the observed state of LookupService.
// Phase 4 publishes the full CRD draft r3 §3 contract: phase +
// conditions + url + installedVersion + deploymentRef.
type LookupServiceStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	Phase ComponentPhase `json:"phase,omitempty"`

	// conditions report the resource's state. Phase 4 publishes:
	//   - Ready                  (aggregate)
	//   - ClusterConfigAvailable (EducatesClusterConfig.Ready gate)
	//   - Deployed               (helm release + Deployment Available)
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// url is the fully-qualified URL the lookup-service Ingress is
	// reachable at. Composed from spec.ingress.prefix and
	// EducatesClusterConfig.status.ingress.domain. Always https in
	// v1alpha1 (the operator always requires a wildcard TLS Secret
	// on the cluster config).
	// +optional
	URL string `json:"url,omitempty"`

	// installedVersion records the lookup-service chart version most
	// recently applied.
	// +optional
	InstalledVersion string `json:"installedVersion,omitempty"`

	// deploymentRef names the upstream Deployment the operator is
	// gating Ready on. Stable across reconciles.
	// +optional
	DeploymentRef *NamespacedRef `json:"deploymentRef,omitempty"`
}

// LookupService is the singleton resource that drives installation of
// the lookup-service component.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="LookupService must be named 'cluster' (singleton per cluster)"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type LookupService struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec LookupServiceSpec `json:"spec"`

	// +optional
	Status LookupServiceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LookupServiceList contains a list of LookupService.
type LookupServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LookupService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LookupService{}, &LookupServiceList{})
}
