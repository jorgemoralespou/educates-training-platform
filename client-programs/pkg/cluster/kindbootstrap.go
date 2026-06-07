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
	// HasReadOnly + ReadOnly map to kind's extraMounts[].readOnly.
	// The pair models a nullable bool without requiring text/template
	// pointer dereference: HasReadOnly=false leaves the field unset
	// (kind defaults to read-write); HasReadOnly=true emits the
	// explicit ReadOnly value into the template.
	HasReadOnly bool
	ReadOnly    bool
}
