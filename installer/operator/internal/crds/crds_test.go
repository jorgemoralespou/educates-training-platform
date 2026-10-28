package crds

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// TestApplyChart_AppliesEveryDocumentOfEveryCRDFile pins that every CRD a
// chart ships reaches the API server, including each document of a crds/ file
// holding several, with the operator's field manager and forced ownership so
// the apply takes over fields Helm set when it first installed the release.
func TestApplyChart_AppliesEveryDocumentOfEveryCRDFile(t *testing.T) {
	chrt := &chart.Chart{
		Metadata: &chart.Metadata{Name: "runtime", Version: "1.0.0"},
		Files: []*common.File{
			{Name: "crds/single.yaml", Data: []byte(crdYAML("ones.example.com"))},
			{Name: "crds/several.yaml", Data: []byte(
				"# leading comment\n---\n" + crdYAML("twos.example.com") +
					"---\n" + crdYAML("threes.example.com") + "---\n")},
			{Name: "README.md", Data: []byte("not a manifest")},
		},
	}

	var applied []string
	cl := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Apply: func(_ context.Context, _ client.WithWatch, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
			o := &client.ApplyOptions{}
			o.ApplyOptions(opts)
			if o.FieldManager != fieldManager {
				t.Errorf("field manager = %q, want %q", o.FieldManager, fieldManager)
			}
			if o.Force == nil || !*o.Force {
				t.Errorf("apply does not force ownership")
			}

			data, err := json.Marshal(obj)
			if err != nil {
				t.Fatalf("marshal applied object: %v", err)
			}
			var meta struct {
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal(data, &meta); err != nil {
				t.Fatalf("unmarshal applied object: %v", err)
			}
			applied = append(applied, meta.Metadata.Name)
			return nil
		},
	}).Build()

	if err := ApplyChart(context.Background(), cl, chrt); err != nil {
		t.Fatalf("ApplyChart: %v", err)
	}

	want := []string{"ones.example.com", "twos.example.com", "threes.example.com"}
	if !slices.Equal(applied, want) {
		t.Errorf("applied CRDs = %v, want %v", applied, want)
	}
}

func crdYAML(name string) string {
	return `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: ` + name + `
spec:
  group: example.com
  scope: Cluster
  names:
    plural: things
    kind: Thing
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
`
}
