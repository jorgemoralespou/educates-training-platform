package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xeipuuv/gojsonschema"
	"gopkg.in/yaml.v2"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1/schemas"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

func (p *ProjectInfo) NewLocalConfigSetCmd() *cobra.Command {
	c := &cobra.Command{
		Args:  cobra.ExactArgs(2),
		Use:   "set PATH VALUE",
		Short: "Set a field in <data-home>/config.yaml by dotted path",
		Long: `Modify a field in the EducatesLocalConfig file. Path is dotted, e.g.
'ingress.domain' or 'operator.logLevel'. Intermediate maps are auto-
created as needed.

VALUE is coerced based on its raw form:
  - 'true' / 'false'          → boolean
  - integer-looking strings   → integer
  - everything else           → string

The full config is re-validated against the EducatesLocalConfig schema
after the change. The write only happens when validation passes, so
type / enum errors are caught up front with the offending field path.`,
		Example: `  educates local config set ingress.domain workshop.test
  educates local config set operator.logLevel debug
  educates local config set lookupService false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLocalConfigSet(cmd.OutOrStdout(), args[0], args[1])
		},
	}
	return c
}

func runLocalConfigSet(w io.Writer, path, rawValue string) error {
	cfgPath := filepath.Join(utils.GetEducatesHomeDir(), "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("read %s (run 'educates local config init' first?): %w", cfgPath, err)
	}

	// Round-trip through yaml.v2 → JSON-friendly map so gojsonschema
	// and yaml.v2 marshalling both work the same way.
	var root map[string]interface{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse %s: %w", cfgPath, err)
	}
	if root == nil {
		root = map[string]interface{}{}
	}

	if err := setByPath(root, path, coerce(rawValue)); err != nil {
		return err
	}

	if err := validateAgainstLocalSchema(root, cfgPath, path); err != nil {
		return err
	}

	out, err := yaml.Marshal(root)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(cfgPath, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", cfgPath, err)
	}
	fmt.Fprintf(w, "%s.%s = %v\n", filepath.Base(cfgPath), path, coerce(rawValue))
	return nil
}

// setByPath walks root by dotted path, creating intermediate maps as
// needed, and sets the leaf to value. yaml.v2 produces
// map[interface{}]interface{} for nested objects from a Unmarshal; we
// normalise every step we touch to map[string]interface{} so the
// re-marshal is clean.
func setByPath(root map[string]interface{}, path string, value interface{}) error {
	segs := strings.Split(path, ".")
	if len(segs) == 0 || segs[0] == "" {
		return fmt.Errorf("empty path")
	}
	cur := root
	for i, s := range segs[:len(segs)-1] {
		next, ok := cur[s]
		if !ok {
			n := map[string]interface{}{}
			cur[s] = n
			cur = n
			continue
		}
		switch m := next.(type) {
		case map[string]interface{}:
			cur = m
		case map[interface{}]interface{}:
			conv := map[string]interface{}{}
			for k, v := range m {
				conv[fmt.Sprint(k)] = v
			}
			cur[s] = conv
			cur = conv
		default:
			return fmt.Errorf("path %q: segment %q is a %T, not a map", path, strings.Join(segs[:i+1], "."), next)
		}
	}
	cur[segs[len(segs)-1]] = value
	return nil
}

// coerce maps the raw CLI string to a typed value. Conservative: only
// bools and pure integers are recognised; anything else stays a string
// so the schema's "type: string" enforcement does the rest.
func coerce(raw string) interface{} {
	switch raw {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return n
	}
	return raw
}

// validateAgainstLocalSchema runs the in-memory map through the embedded
// EducatesLocalConfig schema. On failure, errors mention the offending
// JSON path so the user can see which field rejected the value.
//
// We deliberately validate the WHOLE config, not just the field we set:
// catches cases where the new value collides with another field's
// constraint (e.g. setting `mode` would always fail since the schema
// rejects mode at the top level).
func validateAgainstLocalSchema(root map[string]interface{}, source, attemptedPath string) error {
	// gojsonschema needs JSON-compatible types; yaml.v2 returns
	// map[interface{}]interface{} for objects, which json.Marshal
	// rejects. Normalise.
	normalised := normaliseForJSON(root)

	loader := gojsonschema.NewBytesLoader(schemas.EducatesLocalConfig)
	docLoader := gojsonschema.NewGoLoader(normalised)
	result, err := gojsonschema.Validate(loader, docLoader)
	if err != nil {
		return fmt.Errorf("%s: schema validation: %w", source, err)
	}
	if result.Valid() {
		return nil
	}

	var msgs []string
	for _, e := range result.Errors() {
		msgs = append(msgs, fmt.Sprintf("  - %s: %s", e.Field(), e.Description()))
	}
	return fmt.Errorf(`set %q rejected by schema validation. %s would become invalid:
%s`, attemptedPath, source, strings.Join(msgs, "\n"))
}

// normaliseForJSON converts yaml.v2's map[interface{}]interface{} to
// map[string]interface{} recursively. Duplicates the helper in the
// loader; keeping it local avoids exposing it from pkg/config.
func normaliseForJSON(v interface{}) interface{} {
	switch x := v.(type) {
	case map[interface{}]interface{}:
		m := make(map[string]interface{}, len(x))
		for k, val := range x {
			m[fmt.Sprint(k)] = normaliseForJSON(val)
		}
		return m
	case map[string]interface{}:
		m := make(map[string]interface{}, len(x))
		for k, val := range x {
			m[k] = normaliseForJSON(val)
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
