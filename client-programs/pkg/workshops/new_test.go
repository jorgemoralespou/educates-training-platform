package workshops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestWorkshopNewRejectsUnknownTemplate(t *testing.T) {
	target := filepath.Join(t.TempDir(), "lab-example")

	o := WorkshopNewOptions{Template: "classic", WithEditor: true, WithTerminal: true}

	err := o.Run([]string{target})

	if err == nil {
		t.Fatal("creating a workshop with template \"classic\" succeeded, want an error")
	}

	if !strings.Contains(err.Error(), "hugo") {
		t.Errorf("error = %q, want it to name the \"hugo\" template", err.Error())
	}
}

func TestWorkshopNewGeneratesHugoWorkshop(t *testing.T) {
	target := filepath.Join(t.TempDir(), "lab-example")

	o := WorkshopNewOptions{Template: "hugo", WithEditor: true, WithTerminal: true}

	if err := o.Run([]string{target}); err != nil {
		t.Fatalf("creating a workshop with template \"hugo\": %v", err)
	}

	data, err := os.ReadFile(filepath.Join(target, "resources", "workshop.yaml"))

	if err != nil {
		t.Fatalf("reading the generated workshop definition: %v", err)
	}

	var definition struct {
		Kind     string `json:"kind"`
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
	}

	if err := yaml.Unmarshal(data, &definition); err != nil {
		t.Fatalf("parsing the generated workshop definition: %v", err)
	}

	if definition.Kind != "Workshop" || definition.Metadata.Name != "lab-example" {
		t.Errorf("generated definition is %s %q, want Workshop \"lab-example\"", definition.Kind, definition.Metadata.Name)
	}

	if _, err := os.Stat(filepath.Join(target, "workshop", "content", "00-workshop-overview.md")); err != nil {
		t.Errorf("generated workshop has no instructions page: %v", err)
	}
}
