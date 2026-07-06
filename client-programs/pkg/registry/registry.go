package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"

	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

const hostMirrorTomlTemplate = `[host."http://%s:5000"]
  capabilities = ["pull", "resolve"]
`

const hostRegistryTomlTemplate = `[host."http://%s:5000"]`

// Progress is the minimal surface the registry package uses to surface
// sub-operation detail on the caller's current step line. The caller
// (cmd) owns the step lifecycle (Start/Done/Fail); these functions only
// report intermediate phase text via Update. A nil Progress is valid and
// silences detail — used by callers that don't render progress.
// progress.Step satisfies this interface.
type Progress interface {
	Update(phase string)
}

// report surfaces a phase line on the caller's step, tolerating a nil
// Progress so every call site stays a one-liner.
func report(p Progress, phase string) {
	if p != nil {
		p.Update(phase)
	}
}

const (
	RegistryImageV3               = "docker.io/library/registry:3"
	RegistryConfigTargetPath      = "/etc/distribution/config.yml"
	EducatesNetworkName           = "educates"
	EducatesRegistryContainer     = "educates-registry"
	EducatesControlPlaneContainer = "educates-control-plane"
)

/**
 * This function is used to deploy the registry and link it to the cluster.
 * It is used when creating a new local cluster.
 */
func DeployRegistryAndLinkToCluster(bindIP string, client *kubernetes.Clientset, p Progress) error {

	err := createRegistryContainer(bindIP, p)
	if err != nil {
		return errors.Wrap(err, "failed to deploy registry")
	}

	// This is needed to make containerd use the local registry

	if err = addRegistryConfigToKindNodes("localhost:5001", fmt.Sprintf(hostRegistryTomlTemplate, EducatesRegistryContainer), p); err != nil {
		return errors.Wrap(err, "failed to add registry config to kind nodes")
	}
	if err = addRegistryConfigToKindNodes("registry.default.svc.cluster.local", fmt.Sprintf(hostRegistryTomlTemplate, EducatesRegistryContainer), p); err != nil {
		return errors.Wrap(err, "failed to add registry config to kind nodes")
	}

	// This is needed so that kubernetes nodes can pull images from the local registry
	if err = documentLocalRegistry(client); err != nil {
		return errors.Wrap(err, "failed to document registry config in cluster")
	}

	return nil
}

/**
 * This function is used to deploy a registry.
 * It is used when creating a new local registry.
 * It will not link the registry to the cluster.
 */
func DeployRegistry(bindIP string, p Progress) error {
	err := createRegistryContainer(bindIP, p)
	if err != nil {
		return errors.Wrap(err, "failed to deploy registry")
	}

	return nil
}

/**
 * This private function only creates the registry container.
 */
func createRegistryContainer(bindIP string, p Progress) error {
	ctx := context.Background()

	report(p, "deploying registry container")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(ctx, EducatesRegistryContainer)

	if err == nil {
		// If we can retrieve a container of required name we assume it is
		// running okay. Technically it could be restarting, stopping or
		// have exited and container was not removed, but if that is the case
		// then leave it up to the user to sort out.

		return nil
	}

	report(p, "pulling registry image")

	reader, err := cli.ImagePull(ctx, RegistryImageV3, image.PullOptions{})
	if err != nil {
		return errors.Wrap(err, "cannot pull registry image")
	}

	defer reader.Close()
	// Drain the pull stream so the image fully downloads. The raw layer
	// progress JSON is discarded rather than dumped to stdout, where it
	// would corrupt the caller's progress line; surfacing pull detail
	// under --verbose is a follow-up.
	io.Copy(io.Discard, reader)

	_, err = cli.NetworkInspect(ctx, EducatesNetworkName, network.InspectOptions{})

	if err != nil {
		_, err = cli.NetworkCreate(ctx, EducatesNetworkName, network.CreateOptions{})

		if err != nil {
			return errors.Wrap(err, "cannot create educates network")
		}
	}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			"5000/tcp": []nat.PortBinding{
				{
					HostIP:   bindIP,
					HostPort: "5001",
				},
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "always",
		},
	}

	labels := map[string]string{
		constants.EducatesContainersAppLabelKey:  constants.EducatesContainersAppLabel,
		constants.EducatesContainersRoleLabelKey: constants.EducatesContainersRegistryRoleLabel,
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: RegistryImageV3,
		Tty:   false,
		ExposedPorts: nat.PortSet{
			"5000/tcp": struct{}{},
		},
		Labels: labels,
	}, hostConfig, nil, nil, EducatesRegistryContainer)

	if err != nil {
		return errors.Wrap(err, "cannot create registry container")
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return errors.Wrap(err, "unable to start registry")
	}

	cli.NetworkDisconnect(ctx, EducatesNetworkName, EducatesRegistryContainer, false)

	err = cli.NetworkConnect(ctx, EducatesNetworkName, EducatesRegistryContainer, &network.EndpointSettings{})

	if err != nil {
		return errors.Wrap(err, "unable to connect registry to educates network")
	}

	if err = linkRegistryToClusterNetwork(EducatesRegistryContainer, p); err != nil {
		return errors.Wrap(err, "failed to link registry to cluster")
	}

	return nil
}

/**
 * This function is used to deploy a registry mirror and link it to the cluster.
 * It is used when creating a new local registry mirror.
 */
func DeployMirrorAndLinkToCluster(mirrorConfig *MirrorConfig, p Progress) error {
	err := createMirrorContainer(mirrorConfig, p)

	if err != nil {
		return errors.Wrap(err, "failed to deploy registry mirror "+mirrorConfig.Mirror)
	}

	content := fmt.Sprintf(hostMirrorTomlTemplate, registryMirrorContainerName(mirrorConfig))
	err = addRegistryConfigToKindNodes(mirrorConfig.Mirror, content, p)

	if err != nil {
		report(p, "warning: mirror not added to kind nodes")
	}

	return nil
}

/**
 * This private function only creates the registry mirror container.
 */
func createMirrorContainer(mirrorConfig *MirrorConfig, p Progress) error {
	ctx := context.Background()

	report(p, "deploying mirror container")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	mirrorContainerName := registryMirrorContainerName(mirrorConfig)
	_, err = cli.ContainerInspect(ctx, mirrorContainerName)

	if err == nil {
		// If we can retrieve a container of required name we assume it is
		// running okay. Technically it could be restarting, stopping or
		// have exited and container was not removed, but if that is the case
		// then leave it up to the user to sort out.
		report(p, "mirror container already exists")

		return nil
	}

	// Prepare environment variables for the registry mirror
	envs := []string{}
	mirrorURL := mirrorConfig.URL
	if mirrorURL == "" {
		mirrorURL = mirrorConfig.Mirror
	}
	envs = append(envs, fmt.Sprintf("REGISTRY_PROXY_REMOTEURL=https://%s", mirrorURL))
	if mirrorConfig.Username != "" {
		envs = append(envs, fmt.Sprintf("REGISTRY_PROXY_USERNAME=%s", mirrorConfig.Username))
	}
	if mirrorConfig.Password != "" {
		envs = append(envs, fmt.Sprintf("REGISTRY_PROXY_PASSWORD=%s", mirrorConfig.Password))
	}

	_, err = cli.NetworkInspect(ctx, EducatesNetworkName, network.InspectOptions{})

	if err != nil {
		_, err = cli.NetworkCreate(ctx, EducatesNetworkName, network.CreateOptions{})

		if err != nil {
			return errors.Wrap(err, "cannot create educates network")
		}
	}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			"5000/tcp": []nat.PortBinding{
				{
					HostIP: "127.0.0.1",
					// HostPort: mirrorConfig.Port,
				},
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "always",
		},
	}

	labels := map[string]string{
		constants.EducatesContainersAppLabelKey:      constants.EducatesContainersAppLabel,
		constants.EducatesContainersRoleLabelKey:     constants.EducatesContainersMirrorRoleLabel,
		constants.EducatesContainersMirrorLabelKey:   mirrorConfig.Mirror,
		constants.EducatesContainersURLLabelKey:      mirrorConfig.URL,
		constants.EducatesContainersUsernameLabelKey: mirrorConfig.Username,
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: RegistryImageV3,
		Tty:   false,
		Env:   envs,
		ExposedPorts: nat.PortSet{
			"5000/tcp": struct{}{},
		},
		Labels: labels,
	}, hostConfig, nil, nil, mirrorContainerName)

	if err != nil {
		return errors.Wrap(err, "cannot create local registry mirror container")
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return errors.Wrap(err, "unable to start local registry mirror")
	}

	cli.NetworkDisconnect(ctx, EducatesNetworkName, mirrorContainerName, false)

	err = cli.NetworkConnect(ctx, EducatesNetworkName, mirrorContainerName, &network.EndpointSettings{})

	if err != nil {
		return errors.Wrap(err, "unable to connect local registry mirror to educates network")
	}

	if err = linkRegistryToClusterNetwork(mirrorContainerName, p); err != nil {
		return errors.Wrap(err, "failed to link local registry mirror to cluster")
	}

	return nil
}

/**
 * This function is used to add the registry config to the kind nodes.
 * It is used when creating a new local registry or registry mirror.
 */
func addRegistryConfigToKindNodes(repositoryName string, content string, p Progress) error {
	ctx := context.Background()

	report(p, fmt.Sprintf("adding registry config (%s) to kind nodes", repositoryName))

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerID, _ := getContainerInfo(EducatesControlPlaneContainer)

	registryDir := "/etc/containerd/certs.d/" + repositoryName

	cmdStatement := []string{"mkdir", "-p", registryDir}

	optionsCreateExecuteScript := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmdStatement,
	}

	response, err := cli.ContainerExecCreate(ctx, containerID, optionsCreateExecuteScript)
	if err != nil {
		return errors.Wrap(err, "unable to create exec command")
	}
	hijackedResponse, err := cli.ContainerExecAttach(ctx, response.ID, container.ExecAttachOptions{})
	if err != nil {
		return errors.Wrap(err, "unable to attach exec command")
	}

	hijackedResponse.Close()

	buffer, err := tarFile([]byte(content), path.Join("/etc/containerd/certs.d/"+repositoryName, "hosts.toml"), 0x644)
	if err != nil {
		return err
	}
	err = cli.CopyToContainer(context.Background(),
		containerID, "/",
		buffer,
		container.CopyToContainerOptions{
			AllowOverwriteDirWithFile: true,
		})
	if err != nil {
		return errors.Wrap(err, "unable to copy file to container")
	}

	return nil
}

/**
 * This function is used to remove the registry config from the kind nodes.
 * It is used when deleting a local registry mirror.
 */
func removeRegistryConfigFromKindNodes(repositoryName string, p Progress) error {
	ctx := context.Background()

	report(p, fmt.Sprintf("removing registry config (%s) from kind nodes", repositoryName))

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerID, _ := getContainerInfo(EducatesControlPlaneContainer)

	if containerID == "" {
		return nil
	}

	registryDir := "/etc/containerd/certs.d/" + repositoryName

	cmdStatement := []string{"rm", "-rf", registryDir}

	optionsCreateExecuteScript := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmdStatement,
	}

	response, err := cli.ContainerExecCreate(ctx, containerID, optionsCreateExecuteScript)
	if err != nil {
		return errors.Wrap(err, "unable to create exec command")
	}

	hijackedResponse, err := cli.ContainerExecAttach(ctx, response.ID, container.ExecAttachOptions{})
	if err != nil {
		return errors.Wrap(err, "unable to attach exec command")
	}

	hijackedResponse.Close()

	return nil
}

/**
 * This function is used to document the local registry in the cluster.
 * It is used when creating a new local registry.
 */
func documentLocalRegistry(client *kubernetes.Clientset) error {
	yamlBytes, err := yaml.Marshal(`host: "localhost:5001"`)
	if err != nil {
		return err
	}

	configMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "local-registry-hosting",
			Namespace: "kube-public",
		},
		Data: map[string]string{
			"localRegistryHosting.v1": string(yamlBytes),
		},
	}

	if _, err := client.CoreV1().ConfigMaps("kube-public").Get(context.TODO(), "local-registry-hosting", metav1.GetOptions{}); k8serrors.IsNotFound(err) {
		_, err = client.CoreV1().ConfigMaps("kube-public").Create(context.TODO(), configMap, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, "unable to create local registry hosting config map")
		}
	} else {
		_, err = client.CoreV1().ConfigMaps("kube-public").Update(context.TODO(), configMap, metav1.UpdateOptions{})
		if err != nil {
			return errors.Wrap(err, "unable to update local registry hosting config map")
		}
	}

	return nil
}

/**
 * This function is used to link the registry to the cluster network, which is the kind network.
 * It is used when creating a new local registry or registry mirror containers.
 */
func linkRegistryToClusterNetwork(containerName string, p Progress) error {
	ctx := context.Background()

	report(p, "linking to cluster network")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	cli.NetworkDisconnect(ctx, "kind", containerName, false)

	err = cli.NetworkConnect(ctx, "kind", containerName, &network.EndpointSettings{})

	if err != nil {
		return errors.Wrap(err, "unable to connect registry to cluster network")
	}

	return nil
}

/**
 * This function is used to delete the local registry.
 * It is used when deleting a local registry or deleting all components of the local cluster.
 */
func DeleteRegistry(p Progress) error {
	ctx := context.Background()

	report(p, "deleting registry container")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(ctx, EducatesRegistryContainer)

	if err != nil {
		// If we can't retrieve a container of required name we assume it does
		// not actually exist.

		return nil
	}

	timeout := 30

	err = cli.ContainerStop(ctx, EducatesRegistryContainer, container.StopOptions{Timeout: &timeout})

	// timeout := time.Duration(30) * time.Second

	// err = cli.ContainerStop(ctx, EducatesRegistryContainer, &timeout)

	if err != nil {
		return errors.Wrap(err, "unable to stop registry container")
	}

	err = cli.ContainerRemove(ctx, EducatesRegistryContainer, container.RemoveOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to delete registry container")
	}

	return nil
}

/**
 * This function is used to delete a local registry mirror and unlink it from the cluster.
 * It is used when deleting a local registry mirror.
 */
func DeleteMirrorAndUnlinkFromCluster(mirrorConfig *MirrorConfig, p Progress) error {
	ctx := context.Background()

	report(p, "deleting mirror container")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerName := registryMirrorContainerName(mirrorConfig)
	_, err = cli.ContainerInspect(ctx, containerName)

	if err != nil {
		// If we can't retrieve a container of required name we assume it does
		// not actually exist.

		report(p, "mirror container does not exist")
		return nil
	}

	timeout := 30

	err = cli.ContainerStop(ctx, containerName, container.StopOptions{Timeout: &timeout})

	if err != nil {
		return errors.Wrap(err, "unable to stop registry mirror container "+containerName)
	}

	err = cli.ContainerRemove(ctx, containerName, container.RemoveOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to delete registry mirror container "+containerName)
	}

	// Remove the registry config from the kind nodes
	err = removeRegistryConfigFromKindNodes(mirrorConfig.Mirror, p)

	if err != nil {
		return errors.Wrap(err, "unable to remove registry config from kind nodes")
	}

	return nil
}

func DeleteRegistryMirrors(p Progress) error {
	ctx := context.Background()

	report(p, "deleting mirror containers")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	mirrors, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: registryMirrorLabelFilters(),
	})

	if err != nil {
		return errors.Wrap(err, "unable to list registry mirrors")
	}

	for _, mirror := range mirrors {

		timeout := 30

		err = cli.ContainerStop(ctx, mirror.ID, container.StopOptions{Timeout: &timeout})

		if err != nil {
			return errors.Wrap(err, "unable to stop registry mirror container "+mirror.ID)
		}

		err = cli.ContainerRemove(ctx, mirror.ID, container.RemoveOptions{})

		if err != nil {
			return errors.Wrap(err, "unable to delete registry mirror container "+mirror.ID)
		}

	}

	return nil
}

// RegistryMirror describes a deployed local registry mirror, read back from
// its container labels.
type RegistryMirror struct {
	Name          string
	URL           string
	Username      string
	Status        string
	ContainerName string
}

// registryMirrorLabelFilters is the label selector identifying registry
// mirror containers. Shared by the list and bulk-delete paths so both
// discover exactly the same set.
func registryMirrorLabelFilters() filters.Args {
	return filters.NewArgs(
		filters.Arg("label", constants.EducatesContainersRoleLabelKey+"="+constants.EducatesContainersMirrorRoleLabel),
		filters.Arg("label", constants.EducatesContainersAppLabelKey+"="+constants.EducatesContainersAppLabel),
	)
}

// ListRegistryMirrors returns the deployed local registry mirrors, sorted
// by name. Each mirror's metadata (name, URL, username) is read back from
// the container labels set at deploy time.
func ListRegistryMirrors() ([]RegistryMirror, error) {
	ctx := context.Background()

	cli, err := docker.NewDockerClient()
	if err != nil {
		return nil, errors.Wrap(err, "unable to create docker client")
	}

	mirrors, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: registryMirrorLabelFilters(),
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to list registry mirrors")
	}

	result := make([]RegistryMirror, 0, len(mirrors))
	for _, item := range mirrors {
		name := item.Labels[constants.EducatesContainersMirrorLabelKey]

		url := item.Labels[constants.EducatesContainersURLLabelKey]
		if url == "" {
			url = name
		}

		result = append(result, RegistryMirror{
			Name:          name,
			URL:           url,
			Username:      item.Labels[constants.EducatesContainersUsernameLabelKey],
			Status:        item.Status,
			ContainerName: utils.GetContainerName(item),
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })

	return result, nil
}

/**
 * TODO: Learn whether this is needed or not
 * This function is used to update the registry k8s service.
 * It is used when creating a cluster or a registry in order to update the k8s service to point to the new registry.
 */
func UpdateRegistryK8SService(k8sclient *kubernetes.Clientset) error {
	ctx := context.Background()

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	service := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "registry",
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeClusterIP,
			Ports: []v1.ServicePort{
				{
					Port:       80,
					TargetPort: intstr.FromInt(5001),
				},
			},
		},
	}

	endpointPort := int32(5000)
	endpointPortName := ""
	endpointAppProtocol := "http"
	endpointProtocol := v1.ProtocolTCP

	registryInfo, err := cli.ContainerInspect(ctx, EducatesRegistryContainer)

	if err != nil {
		return errors.Wrapf(err, "unable to inspect container for registry")
	}

	kindNetwork, exists := registryInfo.NetworkSettings.Networks["kind"]

	if !exists {
		return errors.New("registry is not attached to kind network")
	}

	endpointAddresses := []string{kindNetwork.IPAddress}

	endpointSlice := discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "registry-1",
			Labels: map[string]string{
				"kubernetes.io/service-name": "registry",
			},
		},
		AddressType: "IPv4",
		Ports: []discoveryv1.EndpointPort{
			{
				Name:        &endpointPortName,
				AppProtocol: &endpointAppProtocol,
				Protocol:    &endpointProtocol,
				Port:        &endpointPort,
			},
		},
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: endpointAddresses,
			},
		},
	}

	endpointSliceClient := k8sclient.DiscoveryV1().EndpointSlices("default")

	endpointSliceClient.Delete(context.TODO(), "registry-1", *metav1.NewDeleteOptions(0))

	servicesClient := k8sclient.CoreV1().Services("default")

	servicesClient.Delete(context.TODO(), "registry", *metav1.NewDeleteOptions(0))

	_, err = endpointSliceClient.Create(context.TODO(), &endpointSlice, metav1.CreateOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to create registry headless service endpoint")
	}

	_, err = servicesClient.Create(context.TODO(), &service, metav1.CreateOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to create registry headless service")
	}

	return nil
}

func PruneRegistry(p Progress) error {
	ctx := context.Background()

	report(p, "pruning registry storage")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerID, _ := getContainerInfo(EducatesRegistryContainer)

	cmdStatement := []string{"registry", "garbage-collect", RegistryConfigTargetPath, "--delete-untagged=true"}

	optionsCreateExecuteScript := container.ExecOptions{
		AttachStdout: false,
		AttachStderr: false,
		Cmd:          cmdStatement,
	}

	response, err := cli.ContainerExecCreate(ctx, containerID, optionsCreateExecuteScript)
	if err != nil {
		return errors.Wrap(err, "unable to create exec command")
	}
	err = cli.ContainerExecStart(ctx, response.ID, container.ExecStartOptions{})
	if err != nil {
		return errors.Wrap(err, "unable to exec command")
	}

	report(p, "registry pruned")

	return nil
}

/**
 * This function is used to get the container name of a registry mirror.
 */
func registryMirrorContainerName(mirrorConfig *MirrorConfig) string {
	return fmt.Sprintf("%s-mirror-%s", EducatesRegistryContainer, mirrorConfig.Mirror)
}

/**
 * This function is used to get the container id and status of a container.
 * If the container does not exist, it will return an empty string for the container id and status.
 */
func getContainerInfo(containerName string) (containerID string, status string) {
	ctx := context.Background()

	cli, err := docker.NewDockerClient()
	if err != nil {
		panic(err)
	}

	filters := filters.NewArgs()
	filters.Add(
		"name", containerName,
	)

	resp, err := cli.ContainerList(ctx, container.ListOptions{Filters: filters})
	if err != nil {
		panic(err)
	}

	if len(resp) > 0 {
		containerID = resp[0].ID
		containerStatus := strings.Split(resp[0].Status, " ")
		status = containerStatus[0]
	}

	return
}

/**
 * This function is used to tar a file to be copied into a container.
 */
func tarFile(fileContent []byte, basePath string, fileMode int64) (*bytes.Buffer, error) {
	buffer := &bytes.Buffer{}

	zr := gzip.NewWriter(buffer)
	tw := tar.NewWriter(zr)

	hdr := &tar.Header{
		Name: basePath,
		Mode: fileMode,
		Size: int64(len(fileContent)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return buffer, err
	}
	if _, err := tw.Write(fileContent); err != nil {
		return buffer, err
	}

	// produce tar
	if err := tw.Close(); err != nil {
		return buffer, fmt.Errorf("error closing tar file: %w", err)
	}
	// produce gzip
	if err := zr.Close(); err != nil {
		return buffer, fmt.Errorf("error closing gzip file: %w", err)
	}

	return buffer, nil
}
