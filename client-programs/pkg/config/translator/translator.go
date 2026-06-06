// Package translator converts a CLI config kind into the deployable
// outputs: operator chart values + the four platform CRs
// (EducatesClusterConfig, SecretsManager, LookupService, SessionManager).
//
// Each kind has a Translate* method returning *Output. One renderer
// serialises Output to YAML.
//
// Defaulting of environment-dependent fields (e.g. ingress.domain from
// host IP, operator.image.tag from CLI binary version) does NOT happen
// here. Translate consumes whatever the loader produced + any caller-side
// pre-translate defaulting. This keeps the translator deterministic and
// unit-testable.
package translator

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// Output is the internal representation produced by every Translate*. The
// renderer serialises it to YAML.
//
// Each CR map carries the full resource (apiVersion + kind + metadata +
// spec). Nil means "do not deploy" for LookupService; the other three are
// always present for both scenario and escape kinds.
type Output struct {
	OperatorChartValues   map[string]interface{}
	EducatesClusterConfig map[string]interface{}
	SecretsManager        map[string]interface{}
	LookupService         map[string]interface{} // nil = not deployed
	SessionManager        map[string]interface{}
}

// Options carries caller-side inputs that are too environmental for the
// translator to compute on its own.
type Options struct {
	// CASecretName is the name of the Secret in CASecretNamespace that
	// holds the CustomCA's tls.crt + tls.key. Looked up by domain at
	// the call site (typically via secrets.LocalCachedSecretForCertificateAuthority).
	// Required for TranslateLocal; ignored for TranslateEscape (which
	// passes user-declared CRs through verbatim).
	CASecretName string

	// CASecretNamespace is the namespace of the CA Secret. Empty means
	// the operator namespace. For laptop-mode installs aligned with v3,
	// the caller sets this to "educates-secrets".
	CASecretNamespace string
}

// Translate dispatches on kind. Returns ErrUnknownKind if the loaded
// config is one this translator does not yet handle (e.g. the GKE/EKS/
// Inline scenario kinds, which land later in Phase 5).
func Translate(cfg v1alpha1.Config, opts Options) (*Output, error) {
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		return TranslateLocal(c, opts)
	case *v1alpha1.EducatesInlineConfig:
		return TranslateInline(c, opts)
	case *v1alpha1.EducatesConfig:
		return TranslateEscape(c), nil
	default:
		return nil, fmt.Errorf("translator: unknown kind %q", cfg.GetKind())
	}
}

// wrapCR returns a fully-formed CR resource map for the given platform
// apiVersion/kind/spec. metadata.name is always "cluster" — the four
// platform CRs are singletons.
func wrapCR(apiVersion, kind string, spec map[string]interface{}) map[string]interface{} {
	if spec == nil {
		spec = map[string]interface{}{}
	}
	return map[string]interface{}{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   map[string]interface{}{"name": "cluster"},
		"spec":       spec,
	}
}

const (
	apiVersionConfig   = "config.educates.dev/v1alpha1"
	apiVersionPlatform = "platform.educates.dev/v1alpha1"
)
