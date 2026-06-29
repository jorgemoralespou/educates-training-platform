package cmd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

type LocalClusterCreateOptions struct {
	Config         string
	LocalConfig    bool
	Kubeconfig     string
	Context        string
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
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false, "use <data-home>/config.yaml (default when --config is not given)")
	c.Flags().StringVar(&o.Kubeconfig, "kubeconfig", "", "kubeconfig file (defaults to $KUBECONFIG / ~/.kube/config)")
	c.Flags().StringVar(&o.Context, "context", "", "context name to use within the kubeconfig (for the platform deploy tail-call)")
	c.Flags().StringVar(&o.ClusterImage, "kind-cluster-image", "", "docker image to use when booting the kind cluster")
	c.Flags().BoolVar(&o.ClusterOnly, "cluster-only", false, "create kind cluster + registry; skip the platform deploy")
	c.Flags().StringVar(&o.RegistryBindIP, "registry-bind-ip", "127.0.0.1", "bind IP for the always-on localhost:5001 registry")
	c.Flags().DurationVar(&o.Timeout, "timeout", deployer.DefaultTimeout, "per-CR Ready=True wait timeout (passed through to deploy)")
	c.Flags().BoolVar(&o.Verbose, "verbose", false, "show helm SDK debug output on stderr")
	c.MarkFlagsMutuallyExclusive("config", "local-config")

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

	// 0. Preflight: fail fast (before any docker / kind / k8s mutation)
	//    when the cluster already exists, host 80/443 are bound, or a
	//    secure install is missing its CA. The host-port case is the v3
	//    busybox probe — kind itself fails later with a much less
	//    actionable error if Envoy can't publish. The CA case is the one
	//    the deploy looks up later: without this check it would only fail
	//    after the kind cluster and registry already exist.
	// A secure install (the default; ingress.insecure not set) serves
	// HTTPS from a cached CA. Defaults have already run, so an empty
	// domain has become a <host-IP>.nip.io insecure install that needs no
	// CA; only a domain with insecure left false requires one. This is a
	// pure local-cache read, so do it first: a missing CA is reported
	// without touching Docker or the cluster at all.
	if _, _, err := localCASecretIfSecure(cfg); err != nil {
		return err
	}
	clusterConfig := cluster.NewKindClusterConfig(o.Kubeconfig)
	if exists, err := clusterConfig.ClusterExists(); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("kind cluster 'educates' already exists; run 'educates local cluster delete' first or use the existing cluster directly")
	}
	if err := checkHostPortsAvailable(ctx, cfg.Cluster.ListenAddress, o.Verbose, w); err != nil {
		return err
	}

	// The kind, registry, loopback and mirror phases all report through
	// one progress reporter so the create flow reads as a single sequence
	// of `→ … ✓` steps (the platform deploy tail-call builds its own
	// reporter against the same writer). Verbose turns off in-place
	// morphing so kind's full spinner and the per-step detail lines are
	// all preserved rather than overwritten.
	rep := progress.New(w, 0, isStdoutTTY(w) && !o.Verbose)

	// 1. kind bootstrap. kindBootstrapFromConfig builds the focused
	//    KindBootstrapInput from EducatesLocalConfig.Cluster fields the
	//    template reads. By default kind's phases are forwarded onto this
	//    one morphing step line and its footer chatter is suppressed.
	//    Under --verbose the reporter is non-morphing and kind uses its
	//    own full logger (spinner + detail), so the step's committed
	//    header/footer simply frame kind's output.
	bootstrap := kindBootstrapFromConfig(cfg)
	if err := runStep(rep, "creating kind cluster 'educates'", "ready", func(s progress.Step) error {
		onPhase := s.Update
		if o.Verbose {
			onPhase = nil
		}
		return clusterConfig.CreateCluster(bootstrap, o.ClusterImage, onPhase, o.Verbose)
	}); err != nil {
		return err
	}
	client, err := clusterConfig.Config.GetClient()
	if err != nil {
		return err
	}

	// 2. always-on local registry + k8s Service for imgpkg pulls.
	if err := runStep(rep, "bringing up localhost:5001 registry", "ready", func(s progress.Step) error {
		if err := registry.DeployRegistryAndLinkToCluster(o.RegistryBindIP, client, s); err != nil {
			return err
		}
		s.Update("registering cluster service")
		return registry.UpdateRegistryK8SService(client)
	}); err != nil {
		return fmt.Errorf("registry: %w", err)
	}

	// 3. loopback service for hugo livereload (educates serve-workshop).
	if err := runStep(rep, "creating loopback service", "ready", func(s progress.Step) error {
		return cluster.CreateLoopbackService(client, cfg.Ingress.Domain)
	}); err != nil {
		return fmt.Errorf("loopback service: %w", err)
	}

	// 4. registry mirrors declared in config (pull-through caches).
	for _, m := range cfg.Cluster.RegistryMirrors {
		label := "registry mirror " + m.Mirror
		if m.URL != "" {
			label += " → " + m.URL
		}
		mc := registryMirrorFromConfig(m)
		if err := runStep(rep, label, "ready", func(s progress.Step) error {
			return registry.DeployMirrorAndLinkToCluster(&mc, s)
		}); err != nil {
			return fmt.Errorf("mirror %s: %w", m.Mirror, err)
		}
	}

	if o.ClusterOnly {
		rep.Note("cluster + registry ready (--cluster-only; skipped platform deploy)")
		return nil
	}

	// 5. tail-call the v4 deploy. We have the loaded config; rather
	//    than re-loading it inside runDeploy (which would re-do
	//    the host-IP fallback non-deterministically against a freshly
	//    started cluster's IP), translate here and call deployer.Deploy
	//    directly.
	return tailCallDeploy(ctx, w, cfg, configPath, p, o)
}

// loadLocalConfig returns the loaded v4 config + the path it came from
// (used by error messages). Accepts EducatesLocalConfig directly or
// EducatesConfig with target.provider=kind; everything else errors.
func loadLocalConfig(o *LocalClusterCreateOptions) (*v1alpha1.EducatesLocalConfig, string, error) {
	var path string
	// --local-config is the default for laptop create — matches v3
	// behaviour where running the command with no flags pointed at
	// <data-home>/config.yaml. --config still wins when set.
	if o.Config != "" {
		path = o.Config
	} else {
		path = filepath.Join(utils.GetEducatesHomeDir(), "config.yaml")
		if err := config.EnsureLocalConfigFile(utils.GetEducatesHomeDir()); err != nil {
			return nil, "", err
		}
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
		// re-loading there.
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
	// Fills <host-IP>.nip.io and defaults ingress.insecure when the
	// domain was left empty; no-op when the user set one.
	if _, err := maybeApplyHostDomain(cfg); err != nil {
		return err
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
		// The v4 source-of-truth is *bool (so "unset" round-trips
		// through YAML); collapse to the template-friendly pair here.
		if m.ReadOnly != nil {
			mounts[i].HasReadOnly = true
			mounts[i].ReadOnly = *m.ReadOnly
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

// tailCallDeploy translates the already-loaded+defaulted config and
// runs the install. Shares the translate → deploy plumbing with
// runDeploy via translateAndDeploy; configPath isn't used by the shared
// helper (it kept the file-path around for the now-deleted re-load).
func tailCallDeploy(ctx context.Context, w io.Writer, cfg *v1alpha1.EducatesLocalConfig, _ string, _ *ProjectInfo, o *LocalClusterCreateOptions) error {
	// A secure install needs a cached CA; an insecure one serves plain
	// HTTP and needs none. The preflight in runLocalClusterCreate already
	// failed fast on a missing CA, so this is the same idempotent lookup.
	caName, caNS, err := localCASecretIfSecure(cfg)
	if err != nil {
		return err
	}
	return translateAndDeploy(ctx, w, cfg, caName, caNS, true, deployPipelineFlags{
		Kubeconfig: o.Kubeconfig,
		Context:    o.Context,
		Timeout:    o.Timeout,
		Verbose:    o.Verbose,
	})
}
