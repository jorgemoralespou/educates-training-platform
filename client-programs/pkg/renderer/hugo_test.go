package renderer

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// workshopDefinition returns a Workshop definition with the given title, left
// out when empty, and description, and with env as its spec.session.env when
// env isn't nil.
func workshopDefinition(title string, description string, env []interface{}) *unstructured.Unstructured {
	spec := map[string]interface{}{
		"description": description,
	}

	if title != "" {
		spec["title"] = title
	}

	if env != nil {
		spec["session"] = map[string]interface{}{"env": env}
	}

	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "training.educates.dev/v1beta1",
		"kind":       "Workshop",
		"metadata":   map[string]interface{}{"name": "lab"},
		"spec":       spec,
	}}
}

// workshopDirectory returns a workshop directory holding config as its
// config.yaml, or no config.yaml when config is empty.
func workshopDirectory(t *testing.T, config string) string {
	t.Helper()

	dir := t.TempDir()

	if config != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

const pathwaysConfig = `
pathways:
  default: alt
  paths:
    alt:
      title: Alt Pathway Title
      description: Alt pathway description
      steps:
      - 00-workshop-overview
      - 99-workshop-summary
      params:
      - name: PATH_PARAM
        value: from-pathway
      - name: SHARED_PARAM
        value: from-pathway
    other:
      title: Other Pathway Title
      steps:
      - 00-workshop-overview
      params:
      - name: OTHER_PARAM
        value: from-other
params:
- name: TOP_PARAM
  value: from-top-level
- name: SHARED_PARAM
  value: from-top-level
`

func TestWorkshopParams(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		noTitle bool
		env     []interface{}
		pathway string
		want    map[string]string
	}{
		{
			name:   "the default pathway applies its params, name, title and description",
			config: pathwaysConfig,
			want: map[string]string{
				"TOP_PARAM":            "from-top-level",
				"PATH_PARAM":           "from-pathway",
				"SHARED_PARAM":         "from-pathway",
				"pathway_name":         "alt",
				"workshop_title":       "Alt Pathway Title",
				"workshop_description": "Alt pathway description",
			},
		},
		{
			name:   "PATHWAY_NAME in spec.session.env selects another pathway",
			config: pathwaysConfig,
			env: []interface{}{
				map[string]interface{}{"name": "PATHWAY_NAME", "value": "other"},
			},
			want: map[string]string{
				"TOP_PARAM":            "from-top-level",
				"SHARED_PARAM":         "from-top-level",
				"OTHER_PARAM":          "from-other",
				"pathway_name":         "other",
				"workshop_title":       "Other Pathway Title",
				"workshop_description": "Definition description",
			},
		},
		{
			name: "spec.session.env overrides params by name, or by aliases in place of the name",
			config: `
params:
- name: BY_NAME
  value: default
- name: BY_ALIAS
  value: default
  aliases:
  - FIRST_ALIAS
  - SECOND_ALIAS
- name: ALIASES_ONLY
  value: default
  aliases:
  - UNSET_ALIAS
- name: EMPTY_VALUE
  value: default
`,
			env: []interface{}{
				map[string]interface{}{"name": "BY_NAME", "value": "from-env"},
				map[string]interface{}{"name": "SECOND_ALIAS", "value": "from-second-alias"},
				map[string]interface{}{"name": "ALIASES_ONLY", "value": "from-env"},
				map[string]interface{}{"name": "EMPTY_VALUE", "value": ""},
			},
			want: map[string]string{
				"BY_NAME":              "from-env",
				"BY_ALIAS":             "from-second-alias",
				"ALIASES_ONLY":         "default",
				"EMPTY_VALUE":          "",
				"pathway_name":         "",
				"workshop_title":       "Definition Title",
				"workshop_description": "Definition description",
			},
		},
		{
			name:   "spec.session.env values a session works out are left out",
			config: pathwaysConfig,
			env: []interface{}{
				map[string]interface{}{"name": "PATHWAY_NAME", "valueFrom": map[string]interface{}{
					"configMapKeyRef": map[string]interface{}{"name": "settings", "key": "pathway"},
				}},
				map[string]interface{}{"name": "TOP_PARAM", "value": "$(session_namespace)"},
				map[string]interface{}{"name": "PATH_PARAM", "value": "prefix-$(ingress_domain)"},
			},
			want: map[string]string{
				"TOP_PARAM":            "from-top-level",
				"PATH_PARAM":           "from-pathway",
				"SHARED_PARAM":         "from-pathway",
				"pathway_name":         "alt",
				"workshop_title":       "Alt Pathway Title",
				"workshop_description": "Alt pathway description",
			},
		},
		{
			name: "a value a session works out replaces an earlier entry of the same name",
			config: `
params:
- name: TOP_PARAM
  value: default
`,
			env: []interface{}{
				map[string]interface{}{"name": "TOP_PARAM", "value": "literal"},
				map[string]interface{}{"name": "TOP_PARAM", "value": "$(session_namespace)"},
			},
			want: map[string]string{
				"TOP_PARAM":            "default",
				"pathway_name":         "",
				"workshop_title":       "Definition Title",
				"workshop_description": "Definition description",
			},
		},
		{
			name:    "without a title in the definition or the pathway, the title is Workshop",
			noTitle: true,
			want: map[string]string{
				"pathway_name":         "",
				"workshop_title":       "Workshop",
				"workshop_description": "Definition description",
			},
		},
		{
			name:   "a pathway asked for by name replaces PATHWAY_NAME and the default",
			config: pathwaysConfig,
			env: []interface{}{
				map[string]interface{}{"name": "PATHWAY_NAME", "value": "alt"},
			},
			pathway: "other",
			want: map[string]string{
				"TOP_PARAM":            "from-top-level",
				"SHARED_PARAM":         "from-top-level",
				"OTHER_PARAM":          "from-other",
				"pathway_name":         "other",
				"workshop_title":       "Other Pathway Title",
				"workshop_description": "Definition description",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title := "Definition Title"

			if tt.noTitle {
				title = ""
			}

			dir := workshopDirectory(t, tt.config)
			workshop := workshopDefinition(title, "Definition description", tt.env)

			got, err := WorkshopParams(dir, workshop, tt.pathway)

			if err != nil {
				t.Fatalf("WorkshopParams() error = %v", err)
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WorkshopParams() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkshopParamsRejectsUndefinedPathway(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "a pathway pathways.paths doesn't define",
			config:  pathwaysConfig,
			wantErr: `pathway "missing" is not defined in config.yaml, which defines the pathways: alt, other`,
		},
		{
			name: "a config.yaml without pathways",
			config: `
params:
- name: TOP_PARAM
  value: from-top-level
`,
			wantErr: `pathway "missing" is not defined in config.yaml, which defines no pathways`,
		},
		{
			name:    "no config.yaml",
			wantErr: `pathway "missing" is not defined in config.yaml, which defines no pathways`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := workshopDirectory(t, tt.config)
			workshop := workshopDefinition("Definition Title", "Definition description", nil)

			_, err := WorkshopParams(dir, workshop, "missing")

			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("WorkshopParams() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
