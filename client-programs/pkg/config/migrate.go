package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// MaybeMigrateV3 attempts to translate a v3 values.yaml in dataHome
// into a v4 config.yaml in the same dir. Returns nil in three cases:
//
//   1. dataHome has no values.yaml — nothing to migrate (first-time user).
//   2. dataHome has config.yaml — already migrated (or never had v3).
//   3. dataHome has values.yaml + provider ∈ {"", "kind"} — migration
//      ran successfully (config.yaml written, values.yaml renamed to
//      values.yaml.v3-backup).
//
// Returns a user-actionable error when values.yaml is present without
// config.yaml AND the provider is anything else (gke, eks, openshift,
// etc.): the laptop translator only handles the kind case, so
// non-laptop installs need to be re-declared by hand against the v4
// kind ladder.
//
// Callers (render, deploy, cluster create) invoke this before falling
// through to MissingLocalConfigError so a successful migration is
// transparent — the user runs the same command they would have on v3,
// it just works on v4 going forward.
//
// Prints a one-line notice on stderr-style output so the user knows
// the migration happened (the design calls for "silent" in the sense
// of "no prompt", not "invisible").
func MaybeMigrateV3(dataHome string) error {
	v3Path := filepath.Join(dataHome, "values.yaml")
	v4Path := filepath.Join(dataHome, "config.yaml")
	backupPath := filepath.Join(dataHome, "values.yaml.v3-backup")

	if _, err := os.Stat(v4Path); err == nil {
		return nil
	}
	if _, err := os.Stat(v3Path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat %s: %w", v3Path, err)
	}

	body, err := os.ReadFile(v3Path)
	if err != nil {
		return fmt.Errorf("read %s: %w", v3Path, err)
	}
	var v3raw map[string]interface{}
	if err := yaml.Unmarshal(body, &v3raw); err != nil {
		return fmt.Errorf("parse %s as v3 values: %w", v3Path, err)
	}

	provider := strV3Path(v3raw, "clusterInfrastructure", "provider")
	if provider != "" && provider != "kind" {
		return fmt.Errorf(`v3 values.yaml at %s has clusterInfrastructure.provider: %q.

The v4 CLI's silent migration only handles laptop-kind installs
(provider empty or "kind"). For non-laptop installs, declare a v4
config explicitly against one of the kinds in
cli.educates.dev/v1alpha1 and rerun with --config <file>:

  - EducatesLocalConfig    (laptop kind)
  - EducatesInlineConfig   (BYO cluster — any provider, pre-existing
                            cert-manager / ingress / policy engine)
  - EducatesGKEConfig      (GKE Managed: Workload Identity + ACME
                            CloudDNS + Contour + Kyverno)
  - EducatesEKSConfig      (EKS Managed: IRSA + ACME Route53 +
                            Contour + Kyverno)
  - EducatesConfig         (escape hatch, full CRD passthrough)

The original %s file is left untouched; you can keep it as a
reference while re-declaring.`, v3Path, provider, v3Path)
	}

	cfg := translateV3ToV4(v3raw)
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal v4 config: %w", err)
	}
	// Prepend the apiVersion/kind header line so the file reads
	// naturally even though yaml.v2's inline TypeMeta marshal works
	// fine — keeps the file consistent with what `local config init`
	// emits.
	if err := os.WriteFile(v4Path, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", v4Path, err)
	}
	if err := os.Rename(v3Path, backupPath); err != nil {
		return fmt.Errorf("rename %s → %s: %w", v3Path, backupPath, err)
	}

	fmt.Fprintf(os.Stderr, "migrated %s → %s; original saved as %s\n",
		v3Path, v4Path, backupPath)

	// Warn about v3-shape cached CA Secrets (Opaque + ca.crt) that
	// the v4 LocalCachedSecretForCertificateAuthority lookup will
	// silently skip. We can't auto-regenerate them — the v3 file
	// holds only the cert, not the private key cert-manager needs to
	// sign workshop certs — so the user has to re-run
	// 'educates local secrets add ca'.
	if domain := strV3Path(v3raw, "clusterIngress", "domain"); domain != "" {
		warnIfV3CACachePresent(os.Stderr, filepath.Join(dataHome, "secrets"), domain)
	}
	return nil
}

// warnIfV3CACachePresent scans the secrets cache for any file shaped
// like the v3 CA cache (Opaque + ca.crt + matching domain annotation)
// and writes a one-time warning to w. Best-effort — failures (no
// secrets dir, unreadable files) silently no-op; the next CLI op
// that needs the CA will give a clear "no cached CA Secret found"
// error anyway.
//
// w is parameterised for tests; production callers pass os.Stderr.
func warnIfV3CACachePresent(w io.Writer, secretsDir, domain string) {
	entries, err := os.ReadDir(secretsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(secretsDir, e.Name()))
		if err != nil {
			continue
		}
		var raw map[string]interface{}
		if err := yaml.Unmarshal(body, &raw); err != nil {
			continue
		}
		ann := asMap(asMap(raw["metadata"])["annotations"])
		if ann == nil {
			continue
		}
		if d, _ := ann["training.educates.dev/domain"].(string); d != domain {
			continue
		}
		t, _ := raw["type"].(string)
		data := asMap(raw["data"])
		if (t == "Opaque" || t == "") && data["ca.crt"] != nil {
			fmt.Fprintf(w, `WARNING: cached v3-shape CA Secret detected at %s.
The v4 lookup expects kubernetes.io/tls + tls.crt + tls.key. Re-run:

  educates local secrets add ca %s --domain %s

to regenerate the cached CA (auto-generated unless you pass --cert/--key).
`, filepath.Join(secretsDir, e.Name()), domain+"-ca", domain)
			return
		}
	}
}

// translateV3ToV4 builds the v4 EducatesLocalConfig from a v3 values map
// parsed as map[string]interface{}. Missing fields stay zero — the v4
// schema defaults pick up the rest at load time.
func translateV3ToV4(v3 map[string]interface{}) *v1alpha1.EducatesLocalConfig {
	cfg := &v1alpha1.EducatesLocalConfig{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: v1alpha1.APIVersion,
			Kind:       v1alpha1.KindEducatesLocalConfig,
		},
	}

	// ingress
	cfg.Ingress.Domain = strV3Path(v3, "clusterIngress", "domain")

	// cluster
	cfg.Cluster.ListenAddress = strV3Path(v3, "localKindCluster", "listenAddress")
	cfg.Cluster.ApiServer.Address = strV3Path(v3, "localKindCluster", "apiServer", "address")
	cfg.Cluster.ApiServer.Port = intV3Path(v3, "localKindCluster", "apiServer", "port")
	cfg.Cluster.Networking.ServiceSubnet = strV3Path(v3, "localKindCluster", "networking", "serviceSubnet")
	cfg.Cluster.Networking.PodSubnet = strV3Path(v3, "localKindCluster", "networking", "podSubnet")
	for _, m := range listV3Path(v3, "localKindCluster", "volumeMounts") {
		entry := asMap(m)
		vm := v1alpha1.VolumeMount{
			HostPath:      strMap(entry, "hostPath"),
			ContainerPath: strMap(entry, "containerPath"),
		}
		if v, ok := entry["readOnly"].(bool); ok {
			vm.ReadOnly = &v
		}
		cfg.Cluster.VolumeMounts = append(cfg.Cluster.VolumeMounts, vm)
	}
	for _, m := range listV3Path(v3, "localKindCluster", "registryMirrors") {
		entry := asMap(m)
		cfg.Cluster.RegistryMirrors = append(cfg.Cluster.RegistryMirrors, v1alpha1.RegistryMirror{
			Mirror:   strMap(entry, "mirror"),
			URL:      strMap(entry, "url"),
			Username: strMap(entry, "username"),
			Password: strMap(entry, "password"),
			Port:     strMap(entry, "port"),
			BindIP:   strMap(entry, "bindIP"),
		})
	}

	// resolver
	cfg.Resolver.TargetAddress = strV3Path(v3, "localDNSResolver", "targetAddress")
	for _, d := range listV3Path(v3, "localDNSResolver", "extraDomains") {
		if s, ok := d.(string); ok {
			cfg.Resolver.ExtraDomains = append(cfg.Resolver.ExtraDomains, s)
		}
	}

	// imageVersions
	for _, m := range listV3Path(v3, "imageVersions") {
		entry := asMap(m)
		cfg.ImageVersions = append(cfg.ImageVersions, v1alpha1.ImageVersion{
			Name:  strMap(entry, "name"),
			Image: strMap(entry, "image"),
		})
	}

	// websiteStyling (narrow subset only)
	cfg.WebsiteStyling.DefaultTheme = strV3Path(v3, "websiteStyling", "defaultTheme")
	for _, m := range listV3Path(v3, "websiteStyling", "themeDataRefs") {
		entry := asMap(m)
		cfg.WebsiteStyling.ThemeDataRefs = append(cfg.WebsiteStyling.ThemeDataRefs, v1alpha1.ThemeDataRef{
			Namespace: strMap(entry, "namespace"),
			Name:      strMap(entry, "name"),
		})
	}

	// secretPropagation
	for _, s := range listV3Path(v3, "secretPropagation", "imagePullSecretNames") {
		if name, ok := s.(string); ok {
			cfg.SecretPropagation.ImagePullSecretNames = append(cfg.SecretPropagation.ImagePullSecretNames, name)
		}
	}

	return cfg
}

// strV3Path walks v3raw by string keys and returns the leaf as string,
// or "" when any segment is missing / wrong type.
func strV3Path(v3 map[string]interface{}, path ...string) string {
	v, ok := walkV3(v3, path...)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func intV3Path(v3 map[string]interface{}, path ...string) int {
	v, ok := walkV3(v3, path...)
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func listV3Path(v3 map[string]interface{}, path ...string) []interface{} {
	v, ok := walkV3(v3, path...)
	if !ok {
		return nil
	}
	list, _ := v.([]interface{})
	return list
}

func walkV3(v3 map[string]interface{}, path ...string) (interface{}, bool) {
	var cur interface{} = v3
	for _, p := range path {
		m := asMap(cur)
		if m == nil {
			return nil, false
		}
		v, ok := m[p]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

func asMap(v interface{}) map[string]interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		return x
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = val
		}
		return out
	}
	return nil
}

func strMap(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}
