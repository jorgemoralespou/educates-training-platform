kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
{{- if or .ApiServer .Networking }}
networking:
  {{- if .ApiServer.Address }}
  # WARNING: It is _strongly_ recommended that you keep this the default
  # (127.0.0.1) for security reasons. However it is possible to change this.
  apiServerAddress: "{{ .ApiServer.Address }}"
  {{- end }}
  {{- if .ApiServer.Port }}
  # By default the API server listens on a random open port.
  # You may choose a specific port but probably don't need to in most cases.
  # Using a random port makes it easier to spin up multiple clusters.
  apiServerPort: {{ .ApiServer.Port }}
  {{- end }}
  {{- if .Networking.ServiceSubnet }}
  serviceSubnet: "{{ .Networking.ServiceSubnet }}"
  {{- end }}
  {{- if .Networking.PodSubnet }}
  podSubnet: "{{ .Networking.PodSubnet }}"
  {{- end }}
{{- end }}
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
  extraPortMappings:
  - containerPort: 80
    {{- if .ListenAddress }}
    listenAddress: {{ .ListenAddress }}
    {{- end }}
    hostPort: 80
    protocol: TCP
  - containerPort: 443
    {{- if .ListenAddress }}
    listenAddress: {{ .ListenAddress }}
    {{- end }}
    hostPort: 443
    protocol: TCP
  {{- if .VolumeMounts }}
  extraMounts:
  {{- range .VolumeMounts }}
  - hostPath: {{ .HostPath }}
    containerPath: {{ .ContainerPath }}
  {{- end }}
  {{- end }}
containerdConfigPatches:
- |-
  [plugins."io.containerd.grpc.v1.cri".registry]
    config_path = "/etc/containerd/certs.d"
