// Package explain renders kubectl-explain-style field documentation from
// a JSON Schema. It is schema-agnostic: callers pass the schema bytes and
// a dotted field path, and get back a human-readable description of that
// field plus its sub-fields.
//
// It understands the subset of JSON Schema the Educates config schemas
// use: properties, items, enum, default, required, additionalProperties,
// $ref (against #/$defs and other in-document pointers), and the
// allOf/anyOf/oneOf combinators (merged into a union of fields).
package explain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Explain renders the field at the dotted path within the schema. An
// empty path explains the root. A leading "<Title>." on the path is
// tolerated so 'EducatesLocalConfig.ingress' and 'ingress' both work.
func Explain(schemaBytes []byte, path string) (string, error) {
	var root map[string]any
	if err := json.Unmarshal(schemaBytes, &root); err != nil {
		return "", fmt.Errorf("parse schema: %w", err)
	}

	kind := title(root)
	path = strings.TrimPrefix(strings.TrimSpace(path), kind+".")

	node := root
	walked := ""
	if path != "" {
		for _, seg := range strings.Split(path, ".") {
			props := mergedProperties(root, objectify(root, node))
			next, ok := props[seg]
			if !ok {
				return "", notFound(seg, kind, walked, props)
			}
			node = asMap(next)
			walked = joinPath(walked, seg)
		}
	}
	return render(root, node, kind, walked), nil
}

func render(root, node map[string]any, kind, path string) string {
	resolved := resolveRef(root, node, 0)
	var b strings.Builder

	if path == "" {
		fmt.Fprintf(&b, "KIND:     %s\n", kind)
	} else {
		fmt.Fprintf(&b, "FIELD:    %s <%s>\n", path, typeString(root, resolved))
	}

	if enum := enumValues(resolved); len(enum) > 0 {
		fmt.Fprintf(&b, "ENUM:     %s\n", strings.Join(enum, ", "))
	}
	if d, ok := resolved["default"]; ok {
		fmt.Fprintf(&b, "DEFAULT:  %v\n", d)
	}

	b.WriteString("\nDESCRIPTION:\n")
	if desc := str(resolved["description"]); desc != "" {
		b.WriteString(indentText(desc, "    ") + "\n")
	} else {
		b.WriteString("    <no description in schema>\n")
	}

	props := mergedProperties(root, objectify(root, resolved))
	if len(props) > 0 {
		required := requiredSet(resolved)
		b.WriteString("\nFIELDS:\n")
		for _, name := range sortedKeys(props) {
			field := resolveRef(root, asMap(props[name]), 0)
			req := ""
			if required[name] {
				req = " -required-"
			}
			fmt.Fprintf(&b, "  %s <%s>%s\n", name, typeString(root, field), req)
			if line := firstLine(str(field["description"])); line != "" {
				fmt.Fprintf(&b, "    %s\n", line)
			}
		}
	}
	return b.String()
}

// objectify resolves a node to the object whose properties are
// navigable: it follows $ref and unwraps an array to its item schema.
func objectify(root, node map[string]any) map[string]any {
	n := resolveRef(root, node, 0)
	if rawType(n) == "array" {
		if items := asMap(n["items"]); items != nil {
			return objectify(root, items)
		}
	}
	return n
}

// resolveRef follows $ref pointers to their target, guarding against
// cycles with a depth limit.
func resolveRef(root, node map[string]any, depth int) map[string]any {
	if node == nil || depth > 100 {
		return node
	}
	ref := str(node["$ref"])
	if ref == "" {
		return node
	}
	target := resolvePointer(root, ref)
	if target == nil {
		return node
	}
	return resolveRef(root, target, depth+1)
}

// resolvePointer resolves an in-document JSON pointer like
// "#/$defs/EducatesClusterConfigSpec".
func resolvePointer(root map[string]any, ref string) map[string]any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	cur := root
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		cur = asMap(cur[unescapePointer(part)])
		if cur == nil {
			return nil
		}
	}
	return cur
}

// mergedProperties returns the union of a node's own properties and those
// contributed by its allOf/anyOf/oneOf branches, with $refs resolved.
func mergedProperties(root, node map[string]any) map[string]any {
	out := map[string]any{}
	n := resolveRef(root, node, 0)
	for k, v := range asMap(n["properties"]) {
		out[k] = v
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		branches, ok := n[key].([]any)
		if !ok {
			continue
		}
		for _, br := range branches {
			for k, v := range mergedProperties(root, asMap(br)) {
				out[k] = v
			}
		}
	}
	return out
}

// requiredSet collects the directly-required field names. Conditional
// requirements (allOf with if/then) are intentionally not pulled in, as
// they only apply under a condition.
func requiredSet(node map[string]any) map[string]bool {
	out := map[string]bool{}
	if req, ok := node["required"].([]any); ok {
		for _, r := range req {
			out[str(r)] = true
		}
	}
	return out
}

// typeString renders a node's type the way kubectl explain does:
// scalar names as-is, arrays as "[]<elem>", maps as "map[string]<value>",
// and structured nodes as "Object".
func typeString(root, node map[string]any) string {
	n := resolveRef(root, node, 0)
	switch rawType(n) {
	case "array":
		return "[]" + typeString(root, asMap(n["items"]))
	case "object":
		if ap := asMap(n["additionalProperties"]); ap != nil {
			return "map[string]" + typeString(root, ap)
		}
		return "Object"
	case "":
		if asMap(n["items"]) != nil {
			return "[]" + typeString(root, asMap(n["items"]))
		}
		if len(asMap(n["properties"])) > 0 || hasCombinators(n) {
			return "Object"
		}
		if _, ok := n["const"]; ok {
			return "string"
		}
		return "any"
	default:
		return rawType(n)
	}
}

func rawType(node map[string]any) string {
	if node == nil {
		return ""
	}
	switch t := node["type"].(type) {
	case string:
		return t
	case []any:
		if len(t) > 0 {
			return str(t[0])
		}
	}
	return ""
}

func enumValues(node map[string]any) []string {
	raw, ok := node["enum"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, fmt.Sprint(v))
	}
	return out
}

func hasCombinators(node map[string]any) bool {
	for _, k := range []string{"allOf", "anyOf", "oneOf"} {
		if _, ok := node[k]; ok {
			return true
		}
	}
	return false
}

func notFound(seg, kind, walked string, props map[string]any) error {
	loc := kind
	if walked != "" {
		loc = kind + "." + walked
	}
	avail := sortedKeys(props)
	if len(avail) == 0 {
		return fmt.Errorf("%s has no field %q (it has no sub-fields to explain)", loc, seg)
	}
	return fmt.Errorf("%s has no field %q; available fields: %s", loc, seg, strings.Join(avail, ", "))
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func title(root map[string]any) string {
	if t := str(root["title"]); t != "" {
		return t
	}
	return "schema"
}

func joinPath(base, seg string) string {
	if base == "" {
		return seg
	}
	return base + "." + seg
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func indentText(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

func unescapePointer(s string) string {
	s = strings.ReplaceAll(s, "~1", "/")
	return strings.ReplaceAll(s, "~0", "~")
}
