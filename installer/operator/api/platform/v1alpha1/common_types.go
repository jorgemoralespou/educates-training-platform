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

// LogLevel selects the verbosity of a component's logger. Shared across
// all platform-group CRDs.
// +kubebuilder:validation:Enum=debug;info;warn;error
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// ComponentPhase summarises the operator's current activity on a
// platform component. Phases are advisory; conditions carry the
// authoritative state.
// +kubebuilder:validation:Enum=Pending;Installing;Ready;Degraded;Uninstalling
type ComponentPhase string

const (
	ComponentPhasePending      ComponentPhase = "Pending"
	ComponentPhaseInstalling   ComponentPhase = "Installing"
	ComponentPhaseReady        ComponentPhase = "Ready"
	ComponentPhaseDegraded     ComponentPhase = "Degraded"
	ComponentPhaseUninstalling ComponentPhase = "Uninstalling"
)

// LocalObjectReference is a name-only reference to an object in the
// operator namespace (or, for cluster-scoped kinds, to the cluster-
// scoped object). Mirrors the shape used in the config API group;
// duplicated here to avoid cross-group Go coupling.
type LocalObjectReference struct {
	// name of the referent.
	// +required
	Name string `json:"name"`
}

// NamespacedRef points at a namespaced object in the cluster by
// namespace+name. Used in status fields where the operator publishes
// the location of a resource it owns (typically the upstream
// component Deployment) so downstream tooling can discover the
// install without re-deriving the namespace convention.
type NamespacedRef struct {
	// +required
	Namespace string `json:"namespace"`
	// +required
	Name string `json:"name"`
}

// ImageRef declares a chart-render-time image override as a separable
// repository + tag pair. The split shape matches what helm dt
// wrap/unwrap (and similar relocation tools) expect.
type ImageRef struct {
	// +optional
	Repository string `json:"repository,omitempty"`
	// +optional
	Tag string `json:"tag,omitempty"`
}
