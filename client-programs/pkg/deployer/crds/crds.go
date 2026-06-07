// Package crds applies the four platform CRDs from the embedded
// operator chart to the cluster before helm-install.
//
// Why this exists: helm intentionally only installs CRDs from a
// chart's crds/ directory on first install — never on upgrade. After
// a CRD shape change, users would have to run
// `kubectl apply -f installer/charts/educates-installer/crds/` by
// hand for the new schema to land. Owning CRD lifecycle from deploy
// removes that step.
//
// The deploy flow passes SkipCRDs=true to helm so the two paths
// don't fight over CRD ownership.
package crds

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"

	helmchart "helm.sh/helm/v4/pkg/chart/v2"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/apply"
)

// Apply pushes every CRD found in chrt.CRDObjects() via SSA. Returns
// the list of GroupKind+Name strings applied so the deploy summary can
// log them. Idempotent: re-runs converge on the latest schema.
func Apply(ctx context.Context, applier *apply.Client, chrt *helmchart.Chart) ([]string, error) {
	var applied []string
	for _, crdEntry := range chrt.CRDObjects() {
		if crdEntry.File == nil || len(crdEntry.File.Data) == 0 {
			continue
		}
		docs, err := splitYAMLDocs(crdEntry.File.Data)
		if err != nil {
			return nil, fmt.Errorf("split CRD file %s: %w", crdEntry.Filename, err)
		}
		for _, doc := range docs {
			u := &unstructured.Unstructured{}
			if err := yaml.Unmarshal(doc, &u.Object); err != nil {
				return nil, fmt.Errorf("parse CRD doc in %s: %w", crdEntry.Filename, err)
			}
			if u.GetKind() != "CustomResourceDefinition" {
				// Defensive: helm's CRDObjects() should only return
				// objects from crds/, but a malformed chart could
				// slip something through.
				continue
			}
			if _, err := applier.Apply(ctx, u); err != nil {
				return nil, fmt.Errorf("apply CRD %s: %w", u.GetName(), err)
			}
			applied = append(applied, u.GetName())
		}
	}
	return applied, nil
}

// splitYAMLDocs handles multi-document YAML files (---  separated).
// helm doesn't promise that each chart file holds exactly one CRD —
// the upstream convention is one-per-file but the format allows
// stacking.
func splitYAMLDocs(data []byte) ([][]byte, error) {
	dec := yaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	var out [][]byte
	for {
		doc, err := dec.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		out = append(out, doc)
	}
	return out, nil
}
