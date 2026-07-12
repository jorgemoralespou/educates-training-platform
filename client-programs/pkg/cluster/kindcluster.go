package cluster

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/pkg/errors"
	"golang.org/x/exp/slices"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"
	"sigs.k8s.io/kind/pkg/log"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// phaseLogger adapts kind's logger interface to a single status
// callback. kind's status helper (cli.StatusForLogger) only drives its
// terminal spinner when the logger is kind's own *cli.Logger; for any
// other logger it falls back to logging each phase via V(0).Infof
// (" • Ensuring node image  ...", " ✓ Ensuring node image", …). We
// intercept those, clean off the marker/ellipsis, and forward the phase
// text to onPhase so the create flow can surface it on its own single
// progress line. Warnings and errors still reach the user on stderr;
// higher verbosity levels are dropped.
type phaseLogger struct {
	onPhase func(string)
}

func (l phaseLogger) Warn(message string)            { fmt.Fprintln(os.Stderr, message) }
func (l phaseLogger) Warnf(format string, a ...any)  { fmt.Fprintf(os.Stderr, format+"\n", a...) }
func (l phaseLogger) Error(message string)           { fmt.Fprintln(os.Stderr, message) }
func (l phaseLogger) Errorf(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) }
func (l phaseLogger) V(level log.Level) log.InfoLogger {
	return phaseInfo{onPhase: l.onPhase, enabled: level == 0}
}

// phaseInfo is the InfoLogger half of phaseLogger. Only V(0) is enabled,
// so kind skips formatting the noisier debug/trace levels entirely.
type phaseInfo struct {
	onPhase func(string)
	enabled bool
}

func (i phaseInfo) Enabled() bool            { return i.enabled }
func (i phaseInfo) Info(message string)      { i.emit(message) }
func (i phaseInfo) Infof(f string, a ...any) { i.emit(fmt.Sprintf(f, a...)) }

func (i phaseInfo) emit(s string) {
	if !i.enabled || i.onPhase == nil {
		return
	}
	if s = cleanKindStatus(s); s != "" {
		i.onPhase(s)
	}
}

// cleanKindStatus strips the bullet/check/cross marker and trailing
// ellipsis that kind's Status wraps each phase in, leaving just the
// phase text (e.g. "Ensuring node image (kindest/node:v1.36.1) 🖼").
func cleanKindStatus(s string) string {
	s = strings.TrimSpace(s)
	for _, marker := range []string{"•", "✓", "✗"} {
		s = strings.TrimPrefix(s, marker)
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "...")
	return strings.TrimSpace(s)
}

type KindClusterConfig struct {
	Config   ClusterConfig
	provider *cluster.Provider
}

func NewKindClusterConfig(kubeconfig string) *KindClusterConfig {
	fallback := ""

	home, err := os.UserHomeDir()

	if err == nil {
		fallback = filepath.Join(home, clientcmd.RecommendedHomeDir, clientcmd.RecommendedFileName)
	}

	provider := cluster.NewProvider(
		cluster.ProviderWithLogger(cmd.NewLogger()),
	)

	return &KindClusterConfig{ClusterConfig{KubeconfigPath(kubeconfig, fallback), ""}, provider}
}

//go:embed kindclusterconfig.yaml.tpl
var clusterConfigTemplateData string

// ClusterExists reports whether the 'educates' kind cluster currently
// exists. err is set only when the underlying list call failed; the
// existence outcome itself is not an error — callers decide whether
// "exists" or "does not exist" is acceptable for the operation they're
// performing (CreateCluster wants !exists; Delete/Start/Stop/Status
// want exists).
func (o *KindClusterConfig) ClusterExists() (bool, error) {
	clusters, err := o.provider.List()
	if err != nil {
		return false, errors.Wrap(err, "unable to get list of clusters")
	}
	return slices.Contains(clusters, "educates"), nil
}

// CreateCluster boots the 'educates' kind cluster. By default kind's own
// status spinner and end-of-create salutation/usage footer are
// suppressed: each kind phase is forwarded to onPhase (so the caller can
// render it on one line), and the footer chatter is turned off. When
// verbose is true kind's full logger is used instead (spinner + detail
// on stderr) and onPhase is ignored.
func (o *KindClusterConfig) CreateCluster(input *KindBootstrapInput, image string, onPhase func(string), verbose bool) error {
	if exists, err := o.ClusterExists(); err != nil {
		return err
	} else if exists {
		return errors.New("cluster for Educates already exists")
	}

	clusterConfigTemplate, err := template.New("kind-cluster-config").Parse(clusterConfigTemplateData)

	if err != nil {
		return errors.Wrap(err, "failed to parse cluster config template")
	}

	var clusterConfigData bytes.Buffer

	err = clusterConfigTemplate.Execute(&clusterConfigData, input)

	if err != nil {
		return errors.Wrap(err, "failed to generate cluster config")
	}

	// Save the cluster config to a file

	configFileDir := utils.GetEducatesHomeDir()

	err = os.MkdirAll(configFileDir, os.ModePerm)

	if err != nil {
		return errors.Wrapf(err, "unable to create config directory")
	}

	kindConfigPath := filepath.Join(configFileDir, "educates-cluster-config.yaml")
	err = os.WriteFile(kindConfigPath, clusterConfigData.Bytes(), 0644)
	if err != nil {
		return errors.Wrap(err, "failed to write cluster config to file")
	}
	if verbose {
		fmt.Println("Cluster config used is saved to: ", kindConfigPath)
	}

	// Verbose: kind's own logger (terminal spinner + detail). Default:
	// forward each phase to onPhase and stay silent otherwise.
	var logger log.Logger = phaseLogger{onPhase: onPhase}
	if verbose {
		logger = cmd.NewLogger()
	}
	provider := cluster.NewProvider(cluster.ProviderWithLogger(logger))

	if err := provider.Create(
		"educates",
		cluster.CreateWithRawConfig(clusterConfigData.Bytes()),
		cluster.CreateWithNodeImage(image),
		cluster.CreateWithWaitForReady(time.Duration(time.Duration(60)*time.Second)),
		cluster.CreateWithKubeconfigPath(o.Config.Kubeconfig),
		cluster.CreateWithDisplayUsage(false),
		cluster.CreateWithDisplaySalutation(false),
	); err != nil {
		return errors.Wrap(err, "failed to create cluster")
	}

	return nil
}

func (o *KindClusterConfig) DeleteCluster() error {
	if exists, err := o.ClusterExists(); !exists {
		if err != nil {
			return err
		}
		return errors.New("cluster for Educates does not exist")
	}

	fmt.Println("Deleting cluster educates ...")

	if err := o.provider.Delete("educates", o.Config.Kubeconfig); err != nil {
		return errors.Wrapf(err, "failed to delete cluster")
	}

	return nil
}

func (o *KindClusterConfig) StopCluster() error {
	ctx := context.Background()

	if exists, err := o.ClusterExists(); !exists {
		if err != nil {
			return err
		}
		return errors.New("cluster for Educates does not exist")
	}

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(ctx, "educates-control-plane")

	if err != nil {
		return errors.Wrap(err, "no container for Educates cluster")
	}

	fmt.Println("Stopping cluster educates ...")

	timeout := 30

	if err := cli.ContainerStop(ctx, "educates-control-plane", container.StopOptions{Timeout: &timeout}); err != nil {
		return errors.Wrapf(err, "failed to stop cluster")
	}

	// timeout := time.Duration(30) * time.Second

	// if err := cli.ContainerStop(ctx, "educates-control-plane", &timeout); err != nil {
	// 	return errors.Wrapf(err, "failed to stop cluster")
	// }

	return nil
}

func (o *KindClusterConfig) StartCluster() error {
	ctx := context.Background()

	if exists, err := o.ClusterExists(); !exists {
		if err != nil {
			return err
		}
		return errors.New("cluster for Educates does not exist")
	}

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(ctx, "educates-control-plane")

	if err != nil {
		return errors.Wrap(err, "no container for Educates cluster")
	}

	fmt.Println("Starting cluster educates ...")

	if err := cli.ContainerStart(ctx, "educates-control-plane", container.StartOptions{}); err != nil {
		return errors.Wrapf(err, "failed to start cluster")
	}

	return nil
}

func (o *KindClusterConfig) ClusterStatus() error {
	ctx := context.Background()

	if exists, err := o.ClusterExists(); !exists {
		if err != nil {
			return err
		}
		return errors.New("cluster for Educates does not exist")
	}

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerJSON, err := cli.ContainerInspect(ctx, "educates-control-plane")

	if err != nil {
		return errors.Wrap(err, "no container for Educates cluster")
	}

	if containerJSON.State.Running {
		fmt.Println("Educates cluster is Running")
		// if ip, err := config.HostIP(); err == nil {
		// 	fmt.Println("  Cluster IP: ", ip)
		// }
	} else {
		fmt.Println("Educates cluster is NOT Running")
	}

	return nil
}
