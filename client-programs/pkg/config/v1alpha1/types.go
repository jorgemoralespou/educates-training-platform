// Package v1alpha1 defines the CLI-facing configuration kinds for the
// Educates v4 installer. The API group is cli.educates.dev/v1alpha1.
//
// These kinds are translated by the CLI into the operator chart values plus
// the four platform CRs (EducatesClusterConfig, SecretsManager, LookupService,
// SessionManager). They are NOT applied to the cluster directly.
package v1alpha1

const (
	GroupName  = "cli.educates.dev"
	Version    = "v1alpha1"
	APIVersion = GroupName + "/" + Version

	KindEducatesLocalConfig = "EducatesLocalConfig"
)

// TypeMeta carries the apiVersion/kind discriminator. Every CLI config kind
// embeds this for kind-aware loading.
type TypeMeta struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind"       json:"kind"`
}

func (t TypeMeta) GetAPIVersion() string { return t.APIVersion }
func (t TypeMeta) GetKind() string       { return t.Kind }

// Config is the marker interface implemented by every CLI config kind in
// this API group. Loaders return Config; callers type-switch to the concrete
// kind they care about.
type Config interface {
	GetAPIVersion() string
	GetKind() string
}
