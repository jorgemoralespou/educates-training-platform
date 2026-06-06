package cluster

// KindBootstrapInput is the focused payload the kind-cluster template
// reads from. Decouples cluster-bootstrap rendering from any specific
// CLI config kind: 'local cluster create' builds one from
// EducatesLocalConfig; future scenario kinds (GKE/EKS — though they
// would not use kind) or test fixtures build it directly.
type KindBootstrapInput struct {
	ListenAddress string
	ApiServer     KindApiServer
	Networking    KindNetworking
	VolumeMounts  []KindVolumeMount
}

type KindApiServer struct {
	Address string
	Port    int
}

type KindNetworking struct {
	ServiceSubnet string
	PodSubnet     string
}

type KindVolumeMount struct {
	HostPath      string
	ContainerPath string
}
