package chart

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"

	"helm.sh/helm/v4/pkg/chart/common"
	commonutil "helm.sh/helm/v4/pkg/chart/common/util"
	"helm.sh/helm/v4/pkg/engine"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

func TestLoad_EmbeddedChartParses(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c == nil || c.Metadata == nil {
		t.Fatal("chart: empty metadata")
	}
	if got, want := c.Metadata.Name, Name; got != want {
		t.Errorf("chart name = %q, want %q", got, want)
	}
	if c.Metadata.Version == "" {
		t.Error("chart version: empty")
	}
	// Sanity check that templates loaded — operator deployment must be
	// in the chart for the install to do anything.
	var hasDeployment bool
	for _, f := range c.Templates {
		if f.Name == "templates/deployment.yaml" {
			hasDeployment = true
			break
		}
	}
	if !hasDeployment {
		t.Error("chart: templates/deployment.yaml not found")
	}
}

// TestRender_EmbeddedChartParsesStrictly renders the embedded chart with
// default values, as an install would, and parses every rendered document
// strictly. Helm accepts a mapping that repeats a key, but strict
// consumers such as Flux's post-renderer reject the whole release.
func TestRender_EmbeddedChartParsesStrictly(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	renderValues, err := commonutil.ToRenderValues(c, map[string]any{},
		common.ReleaseOptions{Name: Name, Namespace: "educates-installer", IsInstall: true}, nil)
	if err != nil {
		t.Fatalf("build render values: %v", err)
	}
	manifests, err := engine.Render(c, renderValues)
	if err != nil {
		t.Fatalf("render chart: %v", err)
	}
	if _, ok := manifests[Name+"/templates/deployment.yaml"]; !ok {
		t.Fatal("render: templates/deployment.yaml not rendered")
	}

	for name, manifest := range manifests {
		if !strings.HasSuffix(name, ".yaml") {
			continue
		}
		reader := utilyaml.NewYAMLReader(bufio.NewReader(strings.NewReader(manifest)))
		for {
			doc, err := reader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			var out map[string]any
			if err := yaml.UnmarshalStrict(doc, &out); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
	}
}
