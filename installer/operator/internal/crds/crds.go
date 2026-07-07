// Package crds carries a copy of the operator's own CRDs, embedded into the
// binary, and applies them to the cluster at startup.
package crds

import (
	"context"
	"fmt"
	"io/fs"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"embed"
)

//go:embed files/*.yaml
var crdFS embed.FS

// fieldManager is the server-side-apply field owner the operator uses for its
// own CRDs, kept distinct from the manager it uses for platform resources so
// ownership of the CRD schema is unambiguous.
const fieldManager = "educates-installer-operator"

// The operator applies its own CRDs at startup (Apply), so it needs write
// access to them.
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update;patch

// Apply server-side-applies the operator's embedded CRDs so the running
// operator always reconciles its CRD schemas to match its own code —
// regardless of how it was installed.
//
// Helm never updates the chart's crds/ directory on `helm upgrade`, whereas
// GitOps tools (helm template --include-crds) re-apply them on every sync.
// Without this, an imperative upgrade leaves the new operator running against
// stale CRDs — new spec fields silently pruned, new CEL rules absent — while a
// declarative sync of the same chart gets the new schema. Applying the
// embedded CRDs here makes both install paths identical at the exact moment a
// CRD schema changes.
//
// It is idempotent: applying identical CRDs on every restart is a no-op beyond
// taking ownership of the managed fields.
func Apply(ctx context.Context, cl client.Client) error {
	entries, err := fs.ReadDir(crdFS, "files")
	if err != nil {
		return fmt.Errorf("read embedded CRDs: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := crdFS.ReadFile("files/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read embedded CRD %s: %w", entry.Name(), err)
		}

		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal(data, u); err != nil {
			return fmt.Errorf("parse embedded CRD %s: %w", entry.Name(), err)
		}

		// controller-gen emits an empty status stanza and a null
		// creationTimestamp; a server-side apply must not assert ownership of
		// those conversion artifacts, so strip them before applying. Uses the
		// non-deprecated Client.Apply API, matching applySSA in the config
		// controller.
		unstructured.RemoveNestedField(u.Object, "status")
		unstructured.RemoveNestedField(u.Object, "metadata", "creationTimestamp")

		if err := cl.Apply(ctx, client.ApplyConfigurationFromUnstructured(u),
			client.FieldOwner(fieldManager), client.ForceOwnership); err != nil {
			return fmt.Errorf("apply CRD %q: %w", u.GetName(), err)
		}
	}

	return nil
}
