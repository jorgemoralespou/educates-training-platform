package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
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

type LocalClusterCreateV4Options struct {
	Config         string
	LocalConfig    bool
	Kubeconfig     string
	ClusterImage   string
	ClusterOnly    bool
	RegistryBindIP string
	Timeout        time.Duration
	Verbose        bool
}

func (p *ProjectInfo) NewLocalClusterCreateV4Cmd() *cobra.Command {
	var o LocalClusterCreateV4Options

	c := &cobra.Command{
		Args:   cobra.NoArgs,
		Use:    "create-v4",
		Short:  "v4 walking-skeleton: create kind cluster + tail-call admin platform deploy-v4 (experimental)",
		Hidden: true,
		Long: `Walking-skeleton implementation of the v4 laptop create path.

  1. Loads EducatesLocalConfig (or EducatesConfig with target.provider=kind).
  2. Creates the kind cluster (reuses the v3 bootstrap helpers; the
     adapter shim builds a sparse v3 InstallationConfig from the v4
     fields the kind template actually reads).
  3. Brings up the always-on localhost:5001 registry.
  4. Sets up the loopback service for 'educates serve-workshop'.
  5. Tail-calls into the v4 deploy pipeline (helm install operator,
     apply CRs, wait for Ready).

--cluster-only stops after step 4 — useful for testing the v4 deploy
against a hand-prepared cluster.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ip, err := utils.ValidateAndResolveIP(o.RegistryBindIP)
			if err != nil {
				return fmt.Errorf("invalid --registry-bind-ip: %w", err)
			}
			o.RegistryBindIP = ip
			return p.runLocalClusterCreateV4(cmd.Context(), cmd.OutOrStdout(), &o)
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

func (p *ProjectInfo) runLocalClusterCreateV4(ctx context.Context, w io.Writer, o *LocalClusterCreateV4Options) error {
	cfg, configPath, err := loadV4LocalConfig(o)
	if err != nil {
		return err
	}
	if err := applyV4LocalDefaults(cfg, p); err != nil {
		return err
	}

	// 1. kind bootstrap via the v3 helper. The adapter renders the
	//    sparse InstallationConfig the kind template expects from
	//    fields the v4 type carries.
	fmt.Fprintln(w, "→ creating kind cluster 'educates'")
	clusterConfig := cluster.NewKindClusterConfig(o.Kubeconfig)
	if err := clusterConfig.CreateCluster(installationConfigFromV4Local(cfg), o.ClusterImage); err != nil {
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
		mc := registryMirrorFromV4(m)
		if err := registry.DeployMirrorAndLinkToCluster(&mc); err != nil {
			return fmt.Errorf("mirror %s: %w", m.Mirror, err)
		}
	}

	if o.ClusterOnly {
		fmt.Fprintln(w, "✓ cluster + registry ready (--cluster-only; skipped platform deploy)")
		return nil
	}

	// 5. tail-call the v4 deploy. We have the loaded config; rather
	//    than re-loading it inside runDeployV4 (which would re-do
	//    the host-IP fallback non-deterministically against a freshly
	//    started cluster's IP), translate here and call deployer.Deploy
	//    directly.
	fmt.Fprintln(w, "→ tail-calling admin platform deploy-v4")
	return tailCallDeployV4(ctx, w, cfg, configPath, p, o)
}

// loadV4LocalConfig returns the loaded v4 config + the path it came from
// (used by error messages). Accepts EducatesLocalConfig directly or
// EducatesConfig with target.provider=kind; everything else errors.
func loadV4LocalConfig(o *LocalClusterCreateV4Options) (*v1alpha1.EducatesLocalConfig, string, error) {
	var path string
	if o.LocalConfig {
		path = filepath.Join(utils.GetEducatesHomeDir(), "config.yaml")
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			return nil, "", config.MissingLocalConfigError(utils.GetEducatesHomeDir())
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
			return nil, path, fmt.Errorf("%s: EducatesConfig is accepted only with target.provider=kind for laptop create-v4", path)
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
		return nil, path, fmt.Errorf("%s: unsupported kind %q for local cluster create-v4", path, cfg.GetKind())
	}
}

// applyV4LocalDefaults mirrors what render/deploy-v4 do before translation:
// CLI-binary defaults for operator.image, host-IP nip.io for ingress.domain
// when empty.
func applyV4LocalDefaults(cfg *v1alpha1.EducatesLocalConfig, p *ProjectInfo) error {
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

// installationConfigFromV4Local builds the sparse v3 InstallationConfig
// the kind cluster template reads from. Only the LocalKindCluster and
// ClusterSecurity branches the template actually touches are populated.
// Step 9 removes this when the kind helper is rewritten to accept v4
// fields directly.
func installationConfigFromV4Local(cfg *v1alpha1.EducatesLocalConfig) *config.InstallationConfig {
	mounts := make([]config.VolumeMountConfig, len(cfg.Cluster.VolumeMounts))
	for i, m := range cfg.Cluster.VolumeMounts {
		mounts[i] = config.VolumeMountConfig{
			HostPath:      m.HostPath,
			ContainerPath: m.ContainerPath,
			ReadOnly:      m.ReadOnly,
		}
	}
	mirrors := make([]config.RegistryMirrorConfig, len(cfg.Cluster.RegistryMirrors))
	for i, m := range cfg.Cluster.RegistryMirrors {
		mirrors[i] = registryMirrorFromV4(m)
	}
	return &config.InstallationConfig{
		LocalKindCluster: config.LocalKindClusterConfig{
			ListenAddress: cfg.Cluster.ListenAddress,
			ApiServer: config.KindApiServerConfig{
				Address: cfg.Cluster.ApiServer.Address,
				Port:    cfg.Cluster.ApiServer.Port,
			},
			Networking: config.KindNetworkingConfig{
				ServiceSubnet: cfg.Cluster.Networking.ServiceSubnet,
				PodSubnet:     cfg.Cluster.Networking.PodSubnet,
			},
			VolumeMounts:    mounts,
			RegistryMirrors: mirrors,
		},
		// EducatesLocalConfig commits to Kyverno; the kind template
		// only branches on "pod-security-policies" vs "pod-security-
		// standards", so the value here is informational for the
		// template only.
		ClusterSecurity: config.ClusterSecurityConfig{PolicyEngine: "kyverno"},
		// ClusterIngress.Domain is read elsewhere by v3 helpers
		// (CreateLoopbackService takes the domain directly, so we
		// don't need it here for the kind bootstrap path).
	}
}

func registryMirrorFromV4(m v1alpha1.RegistryMirror) config.RegistryMirrorConfig {
	return config.RegistryMirrorConfig{
		Mirror:   m.Mirror,
		URL:      m.URL,
		Username: m.Username,
		Password: m.Password,
		Port:     m.Port,
		BindIP:   m.BindIP,
	}
}

// tailCallDeployV4 mirrors the inner part of runDeployV4 but uses the
// already-defaulted EducatesLocalConfig rather than re-reading from disk.
// Step 9 cleanup factors the shared loader→translate→deploy chain into
// a helper both call sites use.
func tailCallDeployV4(ctx context.Context, w io.Writer, cfg *v1alpha1.EducatesLocalConfig, configPath string, p *ProjectInfo, o *LocalClusterCreateV4Options) error {
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
