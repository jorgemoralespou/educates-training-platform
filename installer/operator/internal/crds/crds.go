// Package crds server-side-applies the CRDs whose schemas the operator keeps
// current: its own, embedded into the binary and applied at startup, and the
// ones each runtime subchart ships in its crds/ directory, applied before the
// subchart's release converges.
package crds

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"embed"
)

//go:embed files/*.yaml
var crdFS embed.FS

// fieldManager is the server-side-apply field owner the operator uses for the
// CRDs it applies, kept distinct from the manager it uses for platform
// resources so ownership of the CRD schema is unambiguous.
const fieldManager = "educates-installer-operator"

// The operator applies its own CRDs at startup (Apply) and the runtime
// subcharts' CRDs before converging their releases (ApplyChart), so it needs
// write access to them.
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

		if err := applyFile(ctx, cl, entry.Name(), data); err != nil {
			return err
		}
	}

	return nil
}

// ApplyChart server-side-applies the CRDs chrt ships in its crds/ directory.
//
// Helm applies a chart's crds/ only when it installs a release; its upgrade
// action never touches them. An operator upgrade would otherwise move a
// runtime release to new images and templates while its CRDs keep the schema
// of the release that first installed them, and the API server would prune
// every field the old schema does not declare. Calling this before each
// converge of the release keeps the CRDs at the schema of the chart the
// operator embeds.
//
// Callers install the release with helm.SkipCRDs, leaving the operator the
// only writer of these CRDs. Helm would otherwise apply them again on a first
// install, without forcing conflicts, and fail on any field this apply owns
// whose value the API server normalises on storage.
//
// It is idempotent: applying unchanged CRDs is a no-op on the server. Nothing
// here deletes a CRD, so uninstalling the release leaves its CRDs, and every
// resource of their kinds, in place.
func ApplyChart(ctx context.Context, cl client.Client, chrt *chart.Chart) error {
	for _, obj := range chrt.CRDObjects() {
		if obj.File == nil {
			continue
		}
		if err := applyFile(ctx, cl, obj.Filename, obj.File.Data); err != nil {
			return err
		}
	}

	return nil
}

// applyFile server-side-applies every CRD in one YAML file, which may hold
// several documents, as a chart's crds/ files may.
func applyFile(ctx context.Context, cl client.Client, name string, data []byte) error {
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read CRD file %s: %w", name, err)
		}

		jsonDoc, err := yaml.YAMLToJSON(doc)
		if err != nil {
			return fmt.Errorf("parse CRD file %s: %w", name, err)
		}
		// A document holding only whitespace or comments, such as the one
		// before a leading "---", converts to null.
		if bytes.Equal(bytes.TrimSpace(jsonDoc), []byte("null")) {
			continue
		}

		u := &unstructured.Unstructured{}
		if err := u.UnmarshalJSON(jsonDoc); err != nil {
			return fmt.Errorf("parse CRD file %s: %w", name, err)
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
}
