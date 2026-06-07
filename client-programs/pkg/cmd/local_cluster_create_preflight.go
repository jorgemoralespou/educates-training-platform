package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/go-connections/nat"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
)

// checkHostPortsAvailable confirms host 80 and 443 are free on the given
// listen address by attempting to start a busybox container with those
// ports bound. Mirrors the v3 'local cluster create' preflight — kind
// otherwise comes up successfully but Envoy can't publish 80/443 and
// the install hangs at IngressReady, leaving users to debug "cluster
// created but nothing reachable".
//
// listenAddress="" defaults to 127.0.0.1 (matches the kind template).
func checkHostPortsAvailable(ctx context.Context, listenAddress string, verbose bool, w io.Writer) error {
	if listenAddress == "" {
		listenAddress = "127.0.0.1"
	}

	cli, err := docker.NewDockerClient()
	if err != nil {
		return fmt.Errorf("port-availability check: docker client: %w", err)
	}

	const probeContainer = "educates-port-availability-check"
	// Remove any leftover probe container from a previous interrupted run.
	_ = cli.ContainerRemove(ctx, probeContainer, container.RemoveOptions{Force: true})

	reader, err := cli.ImagePull(ctx, "docker.io/library/busybox:latest", image.PullOptions{})
	if err != nil {
		return fmt.Errorf("port-availability check: pull busybox: %w", err)
	}
	defer reader.Close()
	if verbose {
		_, _ = io.Copy(w, reader)
	} else {
		_, _ = io.Copy(io.Discard, reader)
	}

	exposed := nat.PortSet{}
	bindings := nat.PortMap{}
	for _, port := range []uint{80, 443} {
		key := nat.Port(fmt.Sprintf("%d/tcp", port))
		exposed[key] = struct{}{}
		bindings[key] = []nat.PortBinding{{HostIP: listenAddress, HostPort: fmt.Sprintf("%d", port)}}
	}

	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image:        "docker.io/library/busybox:latest",
			Cmd:          []string{"/bin/true"},
			Tty:          false,
			ExposedPorts: exposed,
		},
		&container.HostConfig{PortBindings: bindings},
		nil, nil, probeContainer)
	if err != nil {
		return fmt.Errorf("port-availability check: create probe: %w", err)
	}
	defer cli.ContainerRemove(ctx, probeContainer, container.RemoveOptions{Force: true})

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("ports 80/443 not available on %s — another process (an ingress controller, a dev server, Docker Desktop port forwards) is holding them: %w", listenAddress, err)
	}

	statusCh, errCh := cli.ContainerWait(ctx, probeContainer, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("port-availability check: wait for probe: %w", err)
		}
	case <-statusCh:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

