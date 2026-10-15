package localcerts_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// withDataHome points the CLI data home, and so the local secrets cache, at
// a fresh temporary directory and returns it.
func withDataHome(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
	t.Setenv(utils.EducatesCLIDataHomeEnv, dataHome)
	return dataHome
}

func localConfig(domain string, insecure bool) *v1alpha1.EducatesLocalConfig {
	cfg := &v1alpha1.EducatesLocalConfig{}
	cfg.Ingress.Domain = domain
	cfg.Ingress.Insecure = insecure
	return cfg
}

// translatedIngress translates cfg with the plan's translator options and
// returns the EducatesClusterConfig spec.ingress.
func translatedIngress(t *testing.T, cfg *v1alpha1.EducatesLocalConfig, plan *localcerts.Plan) map[string]interface{} {
	t.Helper()
	out, err := translator.Translate(cfg, plan.TranslatorOptions())
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	spec := out.EducatesClusterConfig["spec"].(map[string]interface{})
	return spec["ingress"].(map[string]interface{})
}

// nested walks a nested map by dotted path.
func nested(t *testing.T, root map[string]interface{}, path string) interface{} {
	t.Helper()
	var cur interface{} = root
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]interface{})
		if !ok {
			t.Fatalf("%s: %q is not a map in %v", path, seg, root)
		}
		v, found := m[seg]
		if !found {
			t.Fatalf("%s: %q missing in %v", path, seg, root)
		}
		cur = v
	}
	return cur
}

// cacheCA writes a CA Secret for domain into the local secrets cache, as
// `educates local secrets add ca NAME --domain DOMAIN` does.
func cacheCA(t *testing.T, name, domain string) {
	t.Helper()
	certPEM, keyPEM, err := secrets.GenerateSelfSignedCA("test CA for " + domain)
	if err != nil {
		t.Fatal(err)
	}
	if err := secrets.WriteCachedSecret(secrets.NewCASecret(name, certPEM, keyPEM, domain)); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_Insecure_NoneProviderOverHTTP(t *testing.T) {
	// No CA is cached: an insecure install needs none.
	withDataHome(t)
	cfg := localConfig("workshop.test", true)

	plan, err := localcerts.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := plan.Certificates.Provider, "None"; got != want {
		t.Errorf("plan provider = %q, want %q", got, want)
	}

	ingress := translatedIngress(t, cfg, plan)
	certs := ingress["certificates"].(map[string]interface{})
	if got, want := certs["provider"], "None"; got != want {
		t.Errorf("certificates.provider = %v, want %v", got, want)
	}
	if _, present := certs["bundledCertManager"]; present {
		t.Errorf("bundledCertManager present for an insecure install: %v", certs)
	}
	if got, want := ingress["protocol"], "http"; got != want {
		t.Errorf("ingress.protocol = %v, want %v", got, want)
	}
}

func TestResolve_CachedCAForDomain_BundledCertManagerWithCustomCA(t *testing.T) {
	withDataHome(t)
	cacheCA(t, "workshop-ca", "workshop.test")
	cfg := localConfig("workshop.test", false)

	plan, err := localcerts.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := translator.LocalCertificates{
		Provider: "BundledCertManager",
		CustomCA: &translator.LocalCustomCA{
			CAKeyPairSecretRef: translator.SecretRef{Name: "workshop-ca", Namespace: "educates-secrets"},
		},
	}
	if !reflect.DeepEqual(plan.Certificates, want) {
		t.Errorf("plan certificates = %+v, want %+v", plan.Certificates, want)
	}

	ingress := translatedIngress(t, cfg, plan)
	for path, want := range map[string]interface{}{
		"certificates.provider":                                               "BundledCertManager",
		"certificates.bundledCertManager.issuerType":                          "CustomCA",
		"certificates.bundledCertManager.customCA.caCertificateRef.name":      "workshop-ca",
		"certificates.bundledCertManager.customCA.caCertificateRef.namespace": "educates-secrets",
	} {
		if got := nested(t, ingress, path); got != want {
			t.Errorf("ingress.%s = %v, want %v", path, got, want)
		}
	}
	if _, present := ingress["protocol"]; present {
		t.Errorf("ingress.protocol set for a secure install: %v", ingress["protocol"])
	}
}

func TestResolve_NoCachedCAForDomain_ErrorsWithRemediation(t *testing.T) {
	cases := map[string]func(t *testing.T){
		"empty cache": func(t *testing.T) {},
		"CA for another domain": func(t *testing.T) {
			cacheCA(t, "other-ca", "other.test")
		},
		"CA certificate without a key": func(t *testing.T) {
			// The v3 trust-only shape: Opaque with ca.crt, no key to sign with.
			s := &apiv1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "workshop-ca",
					Annotations: map[string]string{"training.educates.dev/domain": "workshop.test"},
				},
				Type: apiv1.SecretTypeOpaque,
				Data: map[string][]byte{"ca.crt": []byte("certificate")},
			}
			if err := secrets.WriteCachedSecret(s); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, stage := range cases {
		t.Run(name, func(t *testing.T) {
			withDataHome(t)
			stage(t)

			_, err := localcerts.Resolve(localConfig("workshop.test", false))
			if err == nil {
				t.Fatal("Resolve succeeded for a secure install with no usable cached CA")
			}
			for _, want := range []string{
				`no cached CA Secret found for domain "workshop.test"`,
				"educates local secrets add ca <name> --domain workshop.test",
				"educates local config set ingress.insecure true",
				`"educates-secrets" namespace`,
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not mention %q:\n%v", want, err)
				}
			}
		})
	}
}

func TestResolve_SyncsEveryCachedSecret(t *testing.T) {
	for name, insecure := range map[string]bool{"secure": false, "insecure": true} {
		t.Run(name, func(t *testing.T) {
			withDataHome(t)
			cacheCA(t, "workshop-ca", "workshop.test")
			cacheCA(t, "other-ca", "other.test")
			if err := secrets.WriteCachedSecret(secrets.NewDockerRegistrySecret("registry", "registry.test", "user", "pass", "")); err != nil {
				t.Fatal(err)
			}

			plan, err := localcerts.Resolve(localConfig("workshop.test", insecure))
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			want := []string{"other-ca", "registry", "workshop-ca"}
			if !reflect.DeepEqual(plan.SyncSecretNames, want) {
				t.Errorf("plan syncs %v, want %v", plan.SyncSecretNames, want)
			}
		})
	}
}

func TestResolve_LeavesAnAbsentCacheAbsent(t *testing.T) {
	dataHome := withDataHome(t)

	if _, err := localcerts.Resolve(localConfig("workshop.test", true)); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "secrets")); !os.IsNotExist(err) {
		t.Errorf("Resolve created the secrets cache directory (stat error: %v)", err)
	}
}

func TestResolveSync_SyncsEveryCachedSecret(t *testing.T) {
	const config = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
ingress:
  domain: workshop.test
`
	for name, configYAML := range map[string]string{"without a local config": "", "with a local config": config} {
		t.Run(name, func(t *testing.T) {
			dataHome := withDataHome(t)
			if configYAML != "" {
				if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(configYAML), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cacheCA(t, "workshop-ca", "workshop.test")
			if err := secrets.WriteCachedSecret(secrets.NewDockerRegistrySecret("registry", "registry.test", "user", "pass", "")); err != nil {
				t.Fatal(err)
			}

			names, err := localcerts.ResolveSync()
			if err != nil {
				t.Fatalf("ResolveSync: %v", err)
			}
			if want := []string{"registry", "workshop-ca"}; !reflect.DeepEqual(names, want) {
				t.Errorf("sync pushes %v, want %v", names, want)
			}
		})
	}
}

// noteRecorder is a localcerts.Reporter that records each note.
type noteRecorder struct{ notes []string }

func (r *noteRecorder) Note(msg string) { r.notes = append(r.notes, msg) }

func TestPlan_CommitAndReport_PrintNoticesThenWarnings(t *testing.T) {
	plan := &localcerts.Plan{
		Notices:  []string{"first notice", "second notice"},
		Warnings: []string{"a warning"},
	}
	want := []string{"first notice", "second notice", "warning: a warning"}

	var reported noteRecorder
	plan.Report(&reported)
	if !reflect.DeepEqual(reported.notes, want) {
		t.Errorf("Report printed %q, want %q", reported.notes, want)
	}

	var committed noteRecorder
	if err := plan.Commit(&committed); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !reflect.DeepEqual(committed.notes, want) {
		t.Errorf("Commit printed %q, want %q", committed.notes, want)
	}
}
