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
// Excluded on purpose (translator/runtime concerns):
//   - Ingress.Domain — derived from host IP at translate time.
//   - Operator.Image.Tag — derived from the CLI binary version.
//   - Cluster.ListenAddress sub-defaults beyond 127.0.0.1.
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
