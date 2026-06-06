package chart

import (
	"testing"
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
