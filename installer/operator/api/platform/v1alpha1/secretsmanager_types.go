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

// SecretsManagerSpec defines the desired state of SecretsManager.
//
// secrets-manager is a singleton at the pod level (the upstream
// implementation can't scale beyond one replica) so no replicas knob is
// exposed. Image-pull credentials are inherited from
// EducatesClusterConfig.status.imageRegistry.pullSecrets and are not
// duplicated here.
type SecretsManagerSpec struct {
	// image overrides the default image reference. Both fields are
	// optional; defaults come from the chart's appVersion-derived
	// image inventory.
	// +optional
	Image *ImageRef `json:"image,omitempty"`

	// logLevel defaults to info.
	// +kubebuilder:default=info
	// +optional
	LogLevel LogLevel `json:"logLevel,omitempty"`

	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// SecretsManagerStatus defines the observed state of SecretsManager.
// Phase 0 publishes only the minimum surface; richer fields
// (installedVersion, deploymentRef) are added in Phase 4 alongside the
// reconciler that produces them.
type SecretsManagerStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	Phase ComponentPhase `json:"phase,omitempty"`

	// conditions report the resource's state. Standard type "Ready"
	// reflects overall readiness; phase-specific types
	// (ClusterConfigAvailable, Deployed) are added with their producing
	// reconcilers.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// SecretsManager is the singleton resource that drives installation of
// the secrets-manager component.
//
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'cluster'",message="SecretsManager must be named 'cluster' (singleton per cluster)"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type SecretsManager struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SecretsManagerSpec `json:"spec"`

	// +optional
	Status SecretsManagerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SecretsManagerList contains a list of SecretsManager.
type SecretsManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SecretsManager `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SecretsManager{}, &SecretsManagerList{})
}
