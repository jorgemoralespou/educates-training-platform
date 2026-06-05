package translator

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// TranslateEscape converts EducatesConfig (the escape-hatch kind) into the
// deployable output. Pure mechanical YAML slicing: every spec block is
// passed through verbatim. No defaults, no invariants, no field-level
// mapping.
//
// LookupService is omitted from output when the user omitted it from the
// config — its presence is the deploy signal.
func TranslateEscape(cfg *v1alpha1.EducatesConfig) *Output {
	out := &Output{
		OperatorChartValues:   escapeOperatorChartValues(cfg),
		EducatesClusterConfig: wrapCR(apiVersionConfig, "EducatesClusterConfig", normaliseSpec(cfg.EducatesClusterConfig)),
		SecretsManager:        wrapCR(apiVersionPlatform, "SecretsManager", normaliseSpec(cfg.SecretsManager)),
		SessionManager:        wrapCR(apiVersionPlatform, "SessionManager", normaliseSpec(cfg.SessionManager)),
	}
	if cfg.LookupService != nil {
		out.LookupService = wrapCR(apiVersionPlatform, "LookupService", normaliseSpec(cfg.LookupService))
	}
	return out
}

func escapeOperatorChartValues(cfg *v1alpha1.EducatesConfig) map[string]interface{} {
	values := map[string]interface{}{}
	if cfg.Operator.Image.Repository != "" || cfg.Operator.Image.Tag != "" {
		image := map[string]interface{}{}
		if cfg.Operator.Image.Repository != "" {
			image["repository"] = cfg.Operator.Image.Repository
		}
		if cfg.Operator.Image.Tag != "" {
			image["tag"] = cfg.Operator.Image.Tag
		}
		values["image"] = image
	}
	if len(cfg.Operator.ImagePullSecrets) > 0 {
		secrets := make([]interface{}, len(cfg.Operator.ImagePullSecrets))
		for i, name := range cfg.Operator.ImagePullSecrets {
			secrets[i] = map[string]interface{}{"name": name}
		}
		values["imagePullSecrets"] = secrets
	}
	if cfg.Operator.LogLevel != "" {
		values["logLevel"] = cfg.Operator.LogLevel
	}
	return values
}

// normaliseSpec converts yaml.v2's map[interface{}]interface{} values
// inside a parsed CR spec into map[string]interface{} so the renderer can
// emit them with deterministic key ordering. Identity for already-string-
// keyed maps and primitives.
func normaliseSpec(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = normaliseValue(v)
	}
	return out
}

func normaliseValue(v interface{}) interface{} {
	switch x := v.(type) {
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			out[toString(k)] = normaliseValue(val)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			out[k] = normaliseValue(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, val := range x {
			out[i] = normaliseValue(val)
		}
		return out
	default:
		return v
	}
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
