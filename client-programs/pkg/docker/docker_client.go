package docker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/moby/moby/client"
)

var (
	dockerClient *client.Client
	once         sync.Once
	initErr      error
)

func NewDockerClient() (*client.Client, error) {
	once.Do(func() {
		dockerClient, initErr = client.New(
			client.FromEnv,
			client.WithAPIVersionNegotiation(), // <-- This is the fix
		)
	})
	return dockerClient, initErr
}

// CheckDaemonRunning verifies the Docker daemon is reachable, returning an
// actionable error when it is not. Creating the client is lazy — it succeeds
// even when Docker is stopped — so commands that need Docker (local kind
// clusters, the local registry, mirrors, the DNS resolver, docker workshops)
// call this as a preflight. Without it a stopped daemon surfaces a cryptic
// failure deep inside kind ("docker ps ... exit status 1") or the Docker SDK.
func CheckDaemonRunning() error {
	cli, err := NewDockerClient()
	if err != nil {
		return fmt.Errorf("unable to create a Docker client: %w\n\nStart Docker (or your Docker daemon) and try again.", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := cli.Ping(ctx, client.PingOptions{}); err != nil {
		return fmt.Errorf("cannot connect to the Docker daemon — is Docker running?\n\nStart Docker (or your Docker daemon) and try again.\n\nunderlying error: %w", err)
	}

	return nil
}
