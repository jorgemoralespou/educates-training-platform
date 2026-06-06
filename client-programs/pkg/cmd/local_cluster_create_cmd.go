package cmd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

type LocalClusterCreateOptions struct {
	Config         string
	LocalConfig    bool
	Kubeconfig     string
	ClusterImage   string
	ClusterOnly    bool
	RegistryBindIP string
	Timeout        time.Duration
	Verbose        bool
}

func (p *ProjectInfo) NewLocalClusterCreateCmd() *cobra.Command {
	var o LocalClusterCreateOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "create",
		Short: "Create a local kind cluster, bring up the registry, and install Educates",
		Long: `Lays down a full laptop install in one command:

  1. Loads EducatesLocalConfig (or EducatesConfig with target.provider=kind).
  2. Creates the 'educates' kind cluster.
  3. Brings up the always-on localhost:5001 registry.
  4. Sets up the loopback service for 'educates serve-workshop'.
  5. Tail-calls into the platform deploy pipeline
     (helm install operator + apply 4 platform CRs + wait Ready).

--cluster-only stops after step 4 — useful for testing the platform
deploy against a hand-prepared cluster.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ip, err := utils.ValidateAndResolveIP(o.RegistryBindIP)
			if err != nil {
				return fmt.Errorf("invalid --registry-bind-ip: %w", err)
			}
			o.RegistryBindIP = ip
			return p.runLocalClusterCreate(cmd.Context(), cmd.OutOrStdout(), &o)
		},
	}

	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind)")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false, "use <data-home>/config.yaml")
	c.Flags().StringVar(&o.Kubeconfig, "kubeconfig", "", "kubeconfig file (defaults to $KUBECONFIG / ~/.kube/config)")
	c.Flags().StringVar(&o.ClusterImage, "kind-cluster-image", "", "docker image to use when booting the kind cluster")
	c.Flags().BoolVar(&o.ClusterOnly, "cluster-only", false, "create kind cluster + registry; skip the platform deploy")
	c.Flags().StringVar(&o.RegistryBindIP, "registry-bind-ip", "127.0.0.1", "bind IP for the always-on localhost:5001 registry")
	c.Flags().DurationVar(&o.Timeout, "timeout", deployer.DefaultTimeout, "per-CR Ready=True wait timeout (passed through to deploy)")
	c.Flags().BoolVar(&o.Verbose, "verbose", false, "show helm SDK debug output on stderr")
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	c.MarkFlagsOneRequired("config", "local-config")

	return c
}

func (p *ProjectInfo) runLocalClusterCreate(ctx context.Context, w io.Writer, o *LocalClusterCreateOptions) error {
	cfg, configPath, err := loadLocalConfig(o)
	if err != nil {
		return err
	}
	if err := applyLocalDefaults(cfg, p); err != nil {
		return err
	}

	// 1. kind bootstrap. kindBootstrapFromConfig builds the focused
	//    KindBootstrapInput from EducatesLocalConfig.Cluster fields
	//    the template reads.
	fmt.Fprintln(w, "→ creating kind cluster 'educates'")
	clusterConfig := cluster.NewKindClusterConfig(o.Kubeconfig)
	if err := clusterConfig.CreateCluster(kindBootstrapFromConfig(cfg), o.ClusterImage); err != nil {
		return err
	}
	client, err := clusterConfig.Config.GetClient()
	if err != nil {
		return err
	}

	// 2. always-on local registry + k8s Service for imgpkg pulls.
	fmt.Fprintln(w, "→ bringing up localhost:5001 registry")
	if err := registry.DeployRegistryAndLinkToCluster(o.RegistryBindIP, client); err != nil {
		return fmt.Errorf("registry: %w", err)
	}
	if err := registry.UpdateRegistryK8SService(client); err != nil {
		return fmt.Errorf("registry service: %w", err)
	}

	// 3. loopback service for hugo livereload (educates serve-workshop).
	if err := cluster.CreateLoopbackService(client, cfg.Ingress.Domain); err != nil {
		return fmt.Errorf("loopback service: %w", err)
	}

	// 4. registry mirrors declared in config (pull-through caches).
	for _, m := range cfg.Cluster.RegistryMirrors {
		fmt.Fprintf(w, "→ registry mirror %s → %s\n", m.Mirror, m.URL)
		mc := registryMirrorFromConfig(m)
		if err := registry.DeployMirrorAndLinkToCluster(&mc); err != nil {
			return fmt.Errorf("mirror %s: %w", m.Mirror, err)
		}
	}

	if o.ClusterOnly {
		fmt.Fprintln(w, "✓ cluster + registry ready (--cluster-only; skipped platform deploy)")
		return nil
	}

	// 5. tail-call the v4 deploy. We have the loaded config; rather
	//    than re-loading it inside runDeploy (which would re-do
	//    the host-IP fallback non-deterministically against a freshly
	//    started cluster's IP), translate here and call deployer.Deploy
	//    directly.
	fmt.Fprintln(w, "→ tail-calling admin platform deploy")
	return tailCallDeploy(ctx, w, cfg, configPath, p, o)
}

// loadLocalConfig returns the loaded v4 config + the path it came from
// (used by error messages). Accepts EducatesLocalConfig directly or
// EducatesConfig with target.provider=kind; everything else errors.
func loadLocalConfig(o *LocalClusterCreateOptions) (*v1alpha1.EducatesLocalConfig, string, error) {
	var path string
	if o.LocalConfig {
		path = filepath.Join(utils.GetEducatesHomeDir(), "config.yaml")
		if err := config.EnsureLocalConfigFile(utils.GetEducatesHomeDir()); err != nil {
			return nil, "", err
		}
	} else {
		path = o.Config
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, path, err
	}
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		return c, path, nil
	case *v1alpha1.EducatesConfig:
		if c.Target == nil || c.Target.Provider != "kind" {
			return nil, path, fmt.Errorf("%s: EducatesConfig is accepted only with target.provider=kind for laptop create", path)
		}
		// Synthesise a LocalConfig that mirrors the cluster/resolver
		// envelope from the escape kind. The remaining ECC/SessionManager
		// CR fields stay in the escape config and reach the deploy via
		// re-loading there. (Walking-skeleton compromise — step 11 might
		// fold this differently when EducatesInlineConfig lands.)
		return &v1alpha1.EducatesLocalConfig{
			TypeMeta: v1alpha1.TypeMeta{
				APIVersion: v1alpha1.APIVersion,
				Kind:       v1alpha1.KindEducatesLocalConfig,
			},
			Cluster: c.Target.Cluster,
		}, path, nil
	default:
		return nil, path, fmt.Errorf("%s: unsupported kind %q for local cluster create", path, cfg.GetKind())
	}
}

// applyLocalDefaults mirrors what render/deploy do before translation:
// CLI-binary defaults for operator.image, host-IP nip.io for ingress.domain
// when empty.
func applyLocalDefaults(cfg *v1alpha1.EducatesLocalConfig, p *ProjectInfo) error {
	cfg.ApplyCLIDefaults(p.Version, p.ImageRepository)
	if cfg.Ingress.Domain == "" {
		ip, err := hostinfo.DetectHostIP()
		if err != nil {
			return fmt.Errorf("auto-detect host IP for ingress.domain: %w", err)
		}
		cfg.Ingress.Domain = hostinfo.NipDomain(ip)
	}
	return nil
}

// kindBootstrapFromConfig pulls the kind-template inputs from an
// EducatesLocalConfig.
func kindBootstrapFromConfig(cfg *v1alpha1.EducatesLocalConfig) *cluster.KindBootstrapInput {
	mounts := make([]cluster.KindVolumeMount, len(cfg.Cluster.VolumeMounts))
	for i, m := range cfg.Cluster.VolumeMounts {
		mounts[i] = cluster.KindVolumeMount{
			HostPath:      m.HostPath,
			ContainerPath: m.ContainerPath,
		}
	}
	return &cluster.KindBootstrapInput{
		ListenAddress: cfg.Cluster.ListenAddress,
		ApiServer: cluster.KindApiServer{
			Address: cfg.Cluster.ApiServer.Address,
			Port:    cfg.Cluster.ApiServer.Port,
		},
		Networking: cluster.KindNetworking{
			ServiceSubnet: cfg.Cluster.Networking.ServiceSubnet,
			PodSubnet:     cfg.Cluster.Networking.PodSubnet,
		},
		VolumeMounts: mounts,
	}
}

func registryMirrorFromConfig(m v1alpha1.RegistryMirror) registry.MirrorConfig {
	return registry.MirrorConfig{
		Mirror:   m.Mirror,
		URL:      m.URL,
		Username: m.Username,
		Password: m.Password,
		Port:     m.Port,
		BindIP:   m.BindIP,
	}
}

// tailCallDeploy mirrors the inner part of runDeploy but uses the
// already-defaulted EducatesLocalConfig rather than re-reading from disk.
// Step 9 cleanup factors the shared loader→translate→deploy chain into
// a helper both call sites use.
func tailCallDeploy(ctx context.Context, w io.Writer, cfg *v1alpha1.EducatesLocalConfig, configPath string, p *ProjectInfo, o *LocalClusterCreateOptions) error {
	caName, lookupErr := lookupLocalCAByDomain(cfg.Ingress.Domain)
	if lookupErr != nil {
		return lookupErr
	}
	opts := translator.Options{
		CASecretName:      caName,
		CASecretNamespace: LocalCASecretNamespace,
	}
	out, err := translator.Translate(cfg, opts)
	if err != nil {
		return err
	}

	cf := genericclioptions.NewConfigFlags(true)
	if o.Kubeconfig != "" {
		cf.KubeConfig = &o.Kubeconfig
	}
	ns := deployer.OperatorNamespace
	cf.Namespace = &ns

	helmLog := io.Discard
	if o.Verbose {
		helmLog = w
	}

	return deployer.Deploy(ctx, out, deployer.Options{
		Getter:           cf,
		Out:              w,
		HelmLog:          helmLog,
		Timeout:          o.Timeout,
		SyncLocalSecrets: true,
	})
}
