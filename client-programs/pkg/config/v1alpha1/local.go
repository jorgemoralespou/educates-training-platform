package v1alpha1

// EducatesLocalConfig is the laptop-kind-cluster scenario kind. Empty file
// (apiVersion + kind only) is valid; defaults fill in everything else.
//
// Hard exclusions (escalate to EducatesConfig escape hatch): mode,
// target.provider, dns, ACME, imageRegistry.prefix, cluster-service
// discriminators, analytics, dockerDaemon.*, storage.*, network.blockCIDRs,
// workshops.frameAncestors, debug.
type EducatesLocalConfig struct {
	TypeMeta `yaml:",inline"`

	Cluster           LocalClusterConfig           `yaml:"cluster,omitempty"`
	Resolver          LocalResolverConfig          `yaml:"resolver,omitempty"`
	Ingress           LocalIngressConfig           `yaml:"ingress,omitempty"`
	ClusterAdmin      *bool                        `yaml:"clusterAdmin,omitempty"`
	LookupService     *bool                        `yaml:"lookupService,omitempty"`
	ImagePrePuller    *bool                        `yaml:"imagePrePuller,omitempty"`
	WebsiteStyling    LocalWebsiteStylingConfig    `yaml:"websiteStyling,omitempty"`
	SecretPropagation LocalSecretPropagationConfig `yaml:"secretPropagation,omitempty"`
	ImageVersions     []ImageVersion               `yaml:"imageVersions,omitempty"`
	Operator          LocalOperatorConfig          `yaml:"operator,omitempty"`
}

type LocalClusterConfig struct {
	ListenAddress         string            `yaml:"listenAddress,omitempty"`
	RegistryListenAddress string            `yaml:"registryListenAddress,omitempty"`
	ApiServer             ApiServerConfig   `yaml:"apiServer,omitempty"`
	Networking            NetworkingConfig  `yaml:"networking,omitempty"`
	VolumeMounts          []VolumeMount     `yaml:"volumeMounts,omitempty"`
	RegistryMirrors       []RegistryMirror  `yaml:"registryMirrors,omitempty"`
}

type ApiServerConfig struct {
	Address string `yaml:"address,omitempty"`
	Port    int    `yaml:"port,omitempty"`
}

type NetworkingConfig struct {
	ServiceSubnet string `yaml:"serviceSubnet,omitempty"`
	PodSubnet     string `yaml:"podSubnet,omitempty"`
}

type VolumeMount struct {
	HostPath      string `yaml:"hostPath"`
	ContainerPath string `yaml:"containerPath"`
	ReadOnly      *bool  `yaml:"readOnly,omitempty"`
}

// RegistryMirror is the user-declared pull-through cache surface. The
// always-on localhost:5001 mirror is implicit and not represented here.
type RegistryMirror struct {
	Mirror   string `yaml:"mirror"`
	URL      string `yaml:"url,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	Port     string `yaml:"port,omitempty"`
	BindIP   string `yaml:"bindIP,omitempty"`
}

type LocalResolverConfig struct {
	TargetAddress string   `yaml:"targetAddress,omitempty"`
	ExtraDomains  []string `yaml:"extraDomains,omitempty"`
}

type LocalIngressConfig struct {
	Domain string `yaml:"domain,omitempty"`
}

// LocalWebsiteStylingConfig is the narrow subset exposed by EducatesLocalConfig.
// Full styling surface (per-page overrides, HTML snippets) is escape-hatch only.
type LocalWebsiteStylingConfig struct {
	DefaultTheme  string         `yaml:"defaultTheme,omitempty"`
	ThemeDataRefs []ThemeDataRef `yaml:"themeDataRefs,omitempty"`
}

type ThemeDataRef struct {
	Namespace string `yaml:"namespace"`
	Name      string `yaml:"name"`
}

type LocalSecretPropagationConfig struct {
	ImagePullSecretNames []string `yaml:"imagePullSecretNames,omitempty"`
}

type ImageVersion struct {
	Name  string `yaml:"name"`
	Image string `yaml:"image"`
}

type LocalOperatorConfig struct {
	Image            OperatorImage `yaml:"image,omitempty"`
	ImagePullSecrets []string      `yaml:"imagePullSecrets,omitempty"`
	LogLevel         string        `yaml:"logLevel,omitempty"`
}

type OperatorImage struct {
	Repository string `yaml:"repository,omitempty"`
	Tag        string `yaml:"tag,omitempty"`
}

// Static defaults — independent of host environment. Applied after YAML
// unmarshal, before validation.
//
// Two further layers of defaulting are applied by callers (typically the
// command code, not the loader):
//
//   - ApplyCLIDefaults uses the CLI binary's compiled-in version/registry
//     to fill operator.image.{repository,tag} when empty. Deterministic
//     per CLI binary, so safe for GitOps.
//   - ApplyHostDefaults uses the laptop's host IP to fill ingress.domain
//     with a nip.io fallback. Host-specific, NOT safe for GitOps; only
//     applied when the user opted into laptop-convenience mode
//     (`--local-config`).
func (c *EducatesLocalConfig) WithDefaults() *EducatesLocalConfig {
	if c.Cluster.ListenAddress == "" {
		c.Cluster.ListenAddress = "127.0.0.1"
	}
	if c.ClusterAdmin == nil {
		t := true
		c.ClusterAdmin = &t
	}
	if c.LookupService == nil {
		t := true
		c.LookupService = &t
	}
	if c.ImagePrePuller == nil {
		f := false
		c.ImagePrePuller = &f
	}
	if c.Operator.LogLevel == "" {
		c.Operator.LogLevel = "info"
	}
	return c
}

// ApplyCLIDefaults fills in operator.image.{repository,tag} from the CLI
// binary's compiled-in defaults. Deterministic per CLI binary; the output
// is reproducible as long as the same CLI version is used.
//
// repository pattern matches `installer/charts/educates-installer/values.yaml`:
// `<imageRepository>/educates-operator`. tag = the CLI binary's version.
func (c *EducatesLocalConfig) ApplyCLIDefaults(projectVersion, imageRepository string) *EducatesLocalConfig {
	if c.Operator.Image.Repository == "" && imageRepository != "" {
		c.Operator.Image.Repository = imageRepository + "/educates-operator"
	}
	if c.Operator.Image.Tag == "" && projectVersion != "" {
		c.Operator.Image.Tag = projectVersion
	}
	return c
}

// Host-derived defaulting (e.g. ingress.domain ← <host-IP>.nip.io) is
// done at the caller, not on the type — the host probe is an external
// effect that doesn't belong on a value type. See pkg/config/hostinfo.
