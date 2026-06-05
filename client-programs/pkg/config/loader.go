package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1/schemas"
	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v2"
)

// Load reads a CLI config file, validates its apiVersion/kind, runs JSON
// schema validation, then strict-unmarshals into the typed struct. The
// returned value implements v1alpha1.Config; callers type-switch to the
// concrete kind.
func Load(path string) (v1alpha1.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return LoadBytes(data, path)
}

// LoadBytes is the path-free variant — useful for stdin and tests. The
// source string is woven into error messages so users can locate the file.
func LoadBytes(data []byte, source string) (v1alpha1.Config, error) {
	var meta v1alpha1.TypeMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("%s: parse apiVersion/kind: %w", source, err)
	}
	if meta.APIVersion == "" || meta.Kind == "" {
		return nil, fmt.Errorf("%s: missing required field 'apiVersion' or 'kind'", source)
	}
	if meta.APIVersion != v1alpha1.APIVersion {
		return nil, fmt.Errorf("%s: unsupported apiVersion %q (want %q)", source, meta.APIVersion, v1alpha1.APIVersion)
	}

	switch meta.Kind {
	case v1alpha1.KindEducatesLocalConfig:
		return loadEducatesLocalConfig(data, source)
	case v1alpha1.KindEducatesConfig:
		return loadEducatesConfig(data, source)
	default:
		return nil, fmt.Errorf("%s: unknown kind %q for apiVersion %q", source, meta.Kind, meta.APIVersion)
	}
}

// LoadLocal is the typed convenience wrapper for callers that only accept
// EducatesLocalConfig (e.g. `educates local config *` commands).
func LoadLocal(path string) (*v1alpha1.EducatesLocalConfig, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	local, ok := cfg.(*v1alpha1.EducatesLocalConfig)
	if !ok {
		return nil, fmt.Errorf("%s: expected kind %q, got %q",
			path, v1alpha1.KindEducatesLocalConfig, cfg.GetKind())
	}
	return local, nil
}

func loadEducatesLocalConfig(data []byte, source string) (*v1alpha1.EducatesLocalConfig, error) {
	if err := validateAgainstSchema(data, schemas.EducatesLocalConfig, source); err != nil {
		return nil, err
	}
	var cfg v1alpha1.EducatesLocalConfig
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	cfg.WithDefaults()
	return &cfg, nil
}

// loadEducatesConfig loads the escape-hatch kind. No WithDefaults() — the
// design contract is that EducatesConfig is passed through verbatim. Strict
// unmarshal is *not* used: CR-spec fields are untyped maps that carry any
// shape the CRDs accept; the JSON schema is the only enforcer.
func loadEducatesConfig(data []byte, source string) (*v1alpha1.EducatesConfig, error) {
	if err := validateAgainstSchema(data, schemas.EducatesConfig, source); err != nil {
		return nil, err
	}
	var cfg v1alpha1.EducatesConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return &cfg, nil
}

// validateAgainstSchema converts the YAML to a generic Go value, then runs
// it through gojsonschema. We rely on the schema for the readable error
// messages (path + reason + value); yaml.UnmarshalStrict is the safety net
// for any Go-side mismatch.
func validateAgainstSchema(yamlData, schemaBytes []byte, source string) error {
	var raw interface{}
	if err := yaml.Unmarshal(yamlData, &raw); err != nil {
		return fmt.Errorf("%s: parse YAML: %w", source, err)
	}
	// gojsonschema needs JSON-compatible types; yaml.v2 returns
	// map[interface{}]interface{} for objects, which json.Marshal rejects.
	normalised := normaliseForJSON(raw)

	loader := gojsonschema.NewBytesLoader(schemaBytes)
	docLoader := gojsonschema.NewGoLoader(normalised)
	result, err := gojsonschema.Validate(loader, docLoader)
	if err != nil {
		return fmt.Errorf("%s: schema validation error: %w", source, err)
	}
	if result.Valid() {
		return nil
	}

	var msgs []string
	for _, e := range result.Errors() {
		msgs = append(msgs, fmt.Sprintf("  - %s: %s", e.Field(), e.Description()))
	}
	return fmt.Errorf("%s: schema validation failed:\n%s", source, strings.Join(msgs, "\n"))
}

// normaliseForJSON recursively converts yaml.v2's map[interface{}]interface{}
// into map[string]interface{} so the value can be JSON-marshalled (which
// gojsonschema uses internally).
func normaliseForJSON(v interface{}) interface{} {
	switch x := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(x))
		for k, val := range x {
			m[fmt.Sprint(k)] = normaliseForJSON(val)
		}
		return m
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = normaliseForJSON(val)
		}
		return out
	default:
		return v
	}
}
