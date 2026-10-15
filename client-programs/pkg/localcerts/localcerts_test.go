package localcerts_test

import (
	"crypto/x509"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
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

func writeCached(t *testing.T, s *apiv1.Secret) {
	t.Helper()
	if err := secrets.WriteCachedSecret(s); err != nil {
		t.Fatal(err)
	}
}

// cacheRoot registers root as root CA name, as
// `educates local secrets add ca NAME --cert --key` does: its CA trust
// Secret and its CA root key pair.
func cacheRoot(t *testing.T, name string, root *testpki.Certificate) {
	t.Helper()
	writeCached(t, secrets.NewCATrustSecret(name, root.CertPEM))
	writeCached(t, secrets.NewCARootKeyPairSecret(name, root.CertPEM, root.KeyPEM))
}

// removeCached removes Secrets from the local secrets cache.
func removeCached(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := secrets.RemoveCachedSecret(name); err != nil {
			t.Fatal(err)
		}
	}
}

// cacheFiles snapshots the local secrets cache: file name to content.
func cacheFiles(t *testing.T, dataHome string) map[string]string {
	t.Helper()
	files := map[string]string{}
	entries, err := os.ReadDir(filepath.Join(dataHome, "secrets"))
	if os.IsNotExist(err) {
		return files
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dataHome, "secrets", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(data)
	}
	return files
}

// noteRecorder is a localcerts.Reporter that records each note.
type noteRecorder struct{ notes []string }

func (r *noteRecorder) Note(msg string) { r.notes = append(r.notes, msg) }

// resolveAndCommit resolves cfg and commits the plan, as a deploy does,
// returning the plan and the lines the commit printed.
func resolveAndCommit(t *testing.T, cfg *v1alpha1.EducatesLocalConfig) (*localcerts.Plan, []string) {
	t.Helper()
	plan, err := localcerts.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var r noteRecorder
	if err := plan.Commit(&r); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return plan, r.notes
}

// mintedCAKeyPair is a CA key pair read back from the cache.
type mintedCAKeyPair struct {
	secret *apiv1.Secret
	// ca is the CA certificate with its key and issuer, usable to sign
	// test leaves.
	ca    *testpki.Certificate
	chain []*x509.Certificate
}

// cachedCAKeyPair loads the CA key pair name from the cache, checking the
// parts that make it a usable key pair: the chain in tls.crt, the key
// matching its first certificate, and ca.crt holding the chain's root.
func cachedCAKeyPair(t *testing.T, name string, root *testpki.Certificate) mintedCAKeyPair {
	t.Helper()
	s, err := secrets.LoadCachedSecret(name)
	if err != nil {
		t.Fatal(err)
	}
	if s.Type != apiv1.SecretTypeTLS {
		t.Errorf("%s type = %q, want kubernetes.io/tls", name, s.Type)
	}
	chain, err := secrets.ParseCertificatesPEM(s.Data["tls.crt"])
	if err != nil {
		t.Fatalf("%s tls.crt: %v", name, err)
	}
	if len(chain) != 2 || !chain[1].Equal(root.Cert) {
		t.Fatalf("%s tls.crt holds %d certificates, want the CA followed by the root", name, len(chain))
	}
	caCrt, err := secrets.ParseCertificatesPEM(s.Data["ca.crt"])
	if err != nil {
		t.Fatalf("%s ca.crt: %v", name, err)
	}
	if len(caCrt) != 1 || !caCrt[0].Equal(root.Cert) {
		t.Errorf("%s ca.crt is not the root certificate", name)
	}
	key, err := secrets.ParsePrivateKeyPEM(s.Data["tls.key"])
	if err != nil {
		t.Fatalf("%s tls.key: %v", name, err)
	}
	if !secrets.KeyMatchesCertificate(chain[0], key) {
		t.Errorf("%s tls.key does not match its CA certificate", name)
	}
	return mintedCAKeyPair{
		secret: s,
		ca:     &testpki.Certificate{Cert: chain[0], Key: key, Issuer: root},
		chain:  chain,
	}
}

// verifies reports whether leaf verifies to root through the minted CA,
// as a TLS client checking a server certificate for dnsName does.
func verifies(leaf *testpki.Certificate, minted mintedCAKeyPair, root *testpki.Certificate, dnsName string) error {
	roots := x509.NewCertPool()
	roots.AddCert(root.Cert)
	intermediates := x509.NewCertPool()
	intermediates.AddCert(minted.chain[0])
	_, err := leaf.Cert.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		DNSName:       dnsName,
	})
	return err
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

func TestResolve_Root_SelectsTheDomainCAKeyPairAndTheRootTrust(t *testing.T) {
	withDataHome(t)
	cacheRoot(t, "educates-ca", testpki.NewRootCA())
	cfg := localConfig("workshop.test", false)

	plan, err := localcerts.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := translator.LocalCertificates{
		Provider: "BundledCertManager",
		CustomCA: &translator.LocalCustomCA{
			CAKeyPairSecretRef:     translator.SecretRef{Name: "workshop.test-ca-key-pair", Namespace: "educates-secrets"},
			CACertificateSecretRef: &translator.SecretRef{Name: "educates-ca", Namespace: "educates-secrets"},
		},
	}
	if !reflect.DeepEqual(plan.Certificates, want) {
		t.Errorf("plan certificates = %+v, want %+v", plan.Certificates, want)
	}

	ingress := translatedIngress(t, cfg, plan)
	for path, want := range map[string]interface{}{
		"certificates.provider":                                                     "BundledCertManager",
		"certificates.bundledCertManager.issuerType":                                "CustomCA",
		"certificates.bundledCertManager.customCA.caKeyPairSecretRef.name":          "workshop.test-ca-key-pair",
		"certificates.bundledCertManager.customCA.caKeyPairSecretRef.namespace":     "educates-secrets",
		"certificates.bundledCertManager.customCA.caCertificateSecretRef.name":      "educates-ca",
		"certificates.bundledCertManager.customCA.caCertificateSecretRef.namespace": "educates-secrets",
	} {
		if got := nested(t, ingress, path); got != want {
			t.Errorf("ingress.%s = %v, want %v", path, got, want)
		}
	}
	if _, present := ingress["protocol"]; present {
		t.Errorf("ingress.protocol set for a secure install: %v", ingress["protocol"])
	}
}

func TestResolve_NewDomain_MintsAConstrainedCAKeyPairOnCommit(t *testing.T) {
	dataHome := withDataHome(t)
	root := testpki.NewRootCA(testpki.WithValidity(time.Now().Add(-time.Hour), time.Now().AddDate(2, 0, 0)))
	cacheRoot(t, "educates-ca", root)
	before := cacheFiles(t, dataHome)

	plan, err := localcerts.Resolve(localConfig("workshop.test", false))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if after := cacheFiles(t, dataHome); !reflect.DeepEqual(after, before) {
		t.Fatalf("Resolve wrote to the cache: %v", keys(after))
	}

	var r noteRecorder
	if err := plan.Commit(&r); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(r.notes) != 1 || !strings.Contains(r.notes[0], `"workshop.test-ca-key-pair"`) || !strings.Contains(r.notes[0], `"educates-ca"`) {
		t.Errorf("Commit printed %q, want one line naming the minted CA key pair and its root", r.notes)
	}

	minted := cachedCAKeyPair(t, "workshop.test-ca-key-pair", root)
	if got, want := minted.secret.Annotations, map[string]string{
		"training.educates.dev/domain":  "workshop.test",
		"training.educates.dev/root-ca": "educates-ca",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("annotations = %v, want %v", got, want)
	}

	ca := minted.chain[0]
	if err := ca.CheckSignatureFrom(root.Cert); err != nil {
		t.Errorf("CA is not signed by the root: %v", err)
	}
	if !ca.BasicConstraintsValid || !ca.IsCA || ca.MaxPathLen != 0 || !ca.MaxPathLenZero {
		t.Errorf("basic constraints: CA=%v maxPathLen=%d zero=%v, want a CA with path length 0", ca.IsCA, ca.MaxPathLen, ca.MaxPathLenZero)
	}
	if ca.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("CA cannot sign certificates")
	}
	if !ca.PermittedDNSDomainsCritical || !reflect.DeepEqual(ca.PermittedDNSDomains, []string{"workshop.test"}) {
		t.Errorf("name constraint permits %v (critical %v), want only workshop.test, critical", ca.PermittedDNSDomains, ca.PermittedDNSDomainsCritical)
	}
	var excluded []string
	for _, r := range ca.ExcludedIPRanges {
		excluded = append(excluded, r.String())
	}
	if want := []string{"0.0.0.0/0", "::/0"}; !reflect.DeepEqual(excluded, want) {
		t.Errorf("excluded IP ranges = %v, want %v", excluded, want)
	}
	if ca.Subject.CommonName != "workshop.test" {
		t.Errorf("subject common name = %q, want the domain", ca.Subject.CommonName)
	}
	if !ca.NotAfter.Equal(root.Cert.NotAfter) {
		t.Errorf("CA expires %v, want the root's expiry %v", ca.NotAfter, root.Cert.NotAfter)
	}
	if ca.NotBefore.After(time.Now()) {
		t.Errorf("CA is not valid yet: not before %v", ca.NotBefore)
	}

	// Clients accept what the CA signs for the domain and reject anything
	// else, and it cannot sign another CA.
	for _, name := range []string{"*.workshop.test", "workshop.test"} {
		leaf := minted.ca.NewLeaf(testpki.WithDNSNames(name))
		if err := verifies(leaf, minted, root, strings.Replace(name, "*", "portal", 1)); err != nil {
			t.Errorf("leaf for %s does not verify: %v", name, err)
		}
	}
	if err := verifies(minted.ca.NewLeaf(testpki.WithDNSNames("*.other.test")), minted, root, "portal.other.test"); err == nil {
		t.Error("leaf for *.other.test verifies; the name constraint is not enforced")
	}
	ipLeaf := minted.ca.NewLeaf(testpki.WithDNSNames("*.workshop.test"), testpki.WithIPAddresses(net.ParseIP("10.0.0.1")))
	if err := verifies(ipLeaf, minted, root, "portal.workshop.test"); err == nil {
		t.Error("leaf with an IP address verifies; IP ranges are not excluded")
	}
	sub := minted.ca.NewIntermediateCA()
	subLeaf := sub.NewLeaf(testpki.WithDNSNames("*.workshop.test"))
	pool := x509.NewCertPool()
	pool.AddCert(root.Cert)
	inter := x509.NewCertPool()
	inter.AddCert(ca)
	inter.AddCert(sub.Cert)
	if _, err := subLeaf.Cert.Verify(x509.VerifyOptions{Roots: pool, Intermediates: inter, DNSName: "portal.workshop.test"}); err == nil {
		t.Error("a CA signed by the minted CA verifies; path length 0 is not enforced")
	}
}

func TestResolve_CachedCAKeyPair_ReusedWithoutMinting(t *testing.T) {
	dataHome := withDataHome(t)
	cacheRoot(t, "educates-ca", testpki.NewRootCA())
	cfg := localConfig("workshop.test", false)
	resolveAndCommit(t, cfg)
	before := cacheFiles(t, dataHome)

	plan, notes := resolveAndCommit(t, cfg)
	if len(notes) != 0 {
		t.Errorf("second deploy printed %q, want nothing", notes)
	}
	if after := cacheFiles(t, dataHome); !reflect.DeepEqual(after, before) {
		t.Error("second deploy changed the cache")
	}
	if got := plan.Certificates.CustomCA.CAKeyPairSecretRef.Name; got != "workshop.test-ca-key-pair" {
		t.Errorf("plan selects %q, want workshop.test-ca-key-pair", got)
	}
}

func TestResolve_StaleCAKeyPair_Reminted(t *testing.T) {
	cases := map[string]struct {
		// stage replaces the cached root or CA key pair after a first
		// deploy and returns the root the CA key pair must come from.
		stage    func(t *testing.T, root *testpki.Certificate) (rootName string, newRoot *testpki.Certificate)
		wantNote string
	}{
		"root replaced under the same name": {
			stage: func(t *testing.T, _ *testpki.Certificate) (string, *testpki.Certificate) {
				removeCached(t, "educates-ca", "educates-ca-root-key-pair")
				replacement := testpki.NewRootCA()
				cacheRoot(t, "educates-ca", replacement)
				return "educates-ca", replacement
			},
			wantNote: "another root CA",
		},
		"root replaced by another root": {
			stage: func(t *testing.T, _ *testpki.Certificate) (string, *testpki.Certificate) {
				removeCached(t, "educates-ca", "educates-ca-root-key-pair")
				replacement := testpki.NewRootCA()
				cacheRoot(t, "mkcert-ca", replacement)
				return "mkcert-ca", replacement
			},
			wantNote: "another root CA",
		},
		"CA key pair expired": {
			stage: func(t *testing.T, root *testpki.Certificate) (string, *testpki.Certificate) {
				expired := root.NewIntermediateCA(
					testpki.WithCommonName("workshop.test"),
					testpki.WithPermittedDNSDomains("workshop.test"),
					testpki.WithMaxPathLen(0),
					testpki.WithValidity(time.Now().AddDate(0, -2, 0), time.Now().AddDate(0, -1, 0)),
				)
				writeCached(t, secrets.NewCAKeyPairSecret("workshop.test", "educates-ca", expired.CertPEM, expired.KeyPEM, root.CertPEM))
				return "educates-ca", root
			},
			wantNote: "expired",
		},
		"CA key pair unreadable": {
			stage: func(t *testing.T, root *testpki.Certificate) (string, *testpki.Certificate) {
				writeCached(t, &apiv1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "workshop.test-ca-key-pair"},
					Type:       apiv1.SecretTypeTLS,
					Data:       map[string][]byte{"tls.crt": []byte("not a certificate"), "tls.key": []byte("not a key")},
				})
				return "educates-ca", root
			},
			wantNote: "not a valid CA key pair",
		},
		"CA key pair not limited to the domain": {
			stage: func(t *testing.T, root *testpki.Certificate) (string, *testpki.Certificate) {
				unconstrained := root.NewIntermediateCA(testpki.WithMaxPathLen(0))
				writeCached(t, secrets.NewCAKeyPairSecret("workshop.test", "educates-ca", unconstrained.CertPEM, unconstrained.KeyPEM, root.CertPEM))
				return "educates-ca", root
			},
			wantNote: "not limited to",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withDataHome(t)
			root := testpki.NewRootCA()
			cacheRoot(t, "educates-ca", root)
			cfg := localConfig("workshop.test", false)
			resolveAndCommit(t, cfg)

			rootName, wantRoot := tc.stage(t, root)

			plan, notes := resolveAndCommit(t, cfg)
			if len(notes) != 1 || !strings.Contains(notes[0], `"workshop.test-ca-key-pair"`) || !strings.Contains(notes[0], tc.wantNote) {
				t.Errorf("Commit printed %q, want one line re-minting the CA key pair because it is %s", notes, tc.wantNote)
			}
			minted := cachedCAKeyPair(t, "workshop.test-ca-key-pair", wantRoot)
			if err := minted.chain[0].CheckSignatureFrom(wantRoot.Cert); err != nil {
				t.Errorf("re-minted CA is not signed by the current root: %v", err)
			}
			if time.Now().After(minted.chain[0].NotAfter) {
				t.Error("re-minted CA has expired")
			}
			if got := minted.secret.Annotations["training.educates.dev/root-ca"]; got != rootName {
				t.Errorf("root-ca annotation = %q, want %q", got, rootName)
			}
			if got := plan.Certificates.CustomCA.CACertificateSecretRef; got == nil || got.Name != rootName {
				t.Errorf("caCertificateSecretRef = %+v, want %q", got, rootName)
			}
		})
	}
}

func TestResolve_ConstrainedRoot(t *testing.T) {
	t.Run("permitting the domain mints from it", func(t *testing.T) {
		withDataHome(t)
		root := testpki.NewRootCA(testpki.WithPermittedDNSDomains("test"))
		cacheRoot(t, "corporate-ca", root)

		resolveAndCommit(t, localConfig("workshop.test", false))

		minted := cachedCAKeyPair(t, "workshop.test-ca-key-pair", root)
		leaf := minted.ca.NewLeaf(testpki.WithDNSNames("*.workshop.test"))
		if err := verifies(leaf, minted, root, "portal.workshop.test"); err != nil {
			t.Errorf("leaf does not verify through the constrained root: %v", err)
		}
	})

	for name, root := range map[string]*testpki.Certificate{
		"permitting only another domain": testpki.NewRootCA(testpki.WithPermittedDNSDomains("other.test")),
		"excluding the domain":           testpki.NewRootCA(testpki.WithExcludedDNSDomains("workshop.test")),
	} {
		t.Run(name+" fails", func(t *testing.T) {
			dataHome := withDataHome(t)
			cacheRoot(t, "corporate-ca", root)
			before := cacheFiles(t, dataHome)

			_, err := localcerts.Resolve(localConfig("workshop.test", false))
			if err == nil {
				t.Fatal("Resolve minted a CA the root's name constraints exclude")
			}
			for _, want := range []string{`root CA "corporate-ca"`, `"workshop.test"`, "name constraints"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not mention %q:\n%v", want, err)
				}
			}
			if after := cacheFiles(t, dataHome); !reflect.DeepEqual(after, before) {
				t.Error("a refused mint changed the cache")
			}
		})
	}
}

func TestResolve_NoRoot_ErrorsWithRemediation(t *testing.T) {
	cases := map[string]func(t *testing.T){
		"empty cache": func(t *testing.T) {},
		"CA certificate without a key": func(t *testing.T) {
			writeCached(t, secrets.NewCATrustSecret("corporate-ca", testpki.NewRootCA().CertPEM))
		},
		"CA key pair for the domain without its root": func(t *testing.T) {
			root := testpki.NewRootCA()
			ca := root.NewIntermediateCA(testpki.WithPermittedDNSDomains("workshop.test"), testpki.WithMaxPathLen(0))
			writeCached(t, secrets.NewCAKeyPairSecret("workshop.test", "educates-ca", ca.CertPEM, ca.KeyPEM, root.CertPEM))
		},
		"CA cached by an alpha release": func(t *testing.T) {
			ca := testpki.NewRootCA()
			writeCached(t, secrets.NewTLSSecret("workshop.test-ca", ca.CertPEM, ca.KeyPEM, "workshop.test"))
		},
	}
	for name, stage := range cases {
		t.Run(name, func(t *testing.T) {
			withDataHome(t)
			stage(t)

			_, err := localcerts.Resolve(localConfig("workshop.test", false))
			if err == nil {
				t.Fatal("Resolve succeeded for a secure install with no root CA")
			}
			for _, want := range []string{
				`"workshop.test"`,
				"educates local secrets add ca <name>\n",
				`--cert "$(mkcert -CAROOT)/rootCA.pem" --key "$(mkcert -CAROOT)/rootCA-key.pem"`,
				"educates local config set ingress.insecure true",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not mention %q:\n%v", want, err)
				}
			}
		})
	}
}

func TestResolve_MoreThanOneRoot_ErrorsNamingThem(t *testing.T) {
	withDataHome(t)
	cacheRoot(t, "educates-ca", testpki.NewRootCA())
	cacheRoot(t, "mkcert-ca", testpki.NewRootCA())

	_, err := localcerts.Resolve(localConfig("workshop.test", false))
	if err == nil {
		t.Fatal("Resolve chose between two root CAs")
	}
	for _, want := range []string{`"educates-ca"`, `"mkcert-ca"`, "one root CA"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

func TestResolve_UnusableRoot_Errors(t *testing.T) {
	root := testpki.NewRootCA()
	cases := map[string]struct {
		stage func(t *testing.T)
		want  string
	}{
		"key does not match": {
			stage: func(t *testing.T) {
				writeCached(t, secrets.NewCATrustSecret("educates-ca", root.CertPEM))
				writeCached(t, secrets.NewCARootKeyPairSecret("educates-ca", root.CertPEM, testpki.NewRootCA().KeyPEM))
			},
			want: "does not match",
		},
		"expired": {
			stage: func(t *testing.T) {
				cacheRoot(t, "educates-ca", testpki.NewRootCA(testpki.WithValidity(time.Now().AddDate(-2, 0, 0), time.Now().AddDate(-1, 0, 0))))
			},
			want: "expired",
		},
		"CA trust Secret missing": {
			stage: func(t *testing.T) {
				writeCached(t, secrets.NewCARootKeyPairSecret("educates-ca", root.CertPEM, root.KeyPEM))
			},
			want: `CA trust Secret "educates-ca"`,
		},
		"CA trust Secret holds another certificate": {
			stage: func(t *testing.T) {
				writeCached(t, secrets.NewCATrustSecret("educates-ca", testpki.NewRootCA().CertPEM))
				writeCached(t, secrets.NewCARootKeyPairSecret("educates-ca", root.CertPEM, root.KeyPEM))
			},
			want: `CA trust Secret "educates-ca"`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withDataHome(t)
			tc.stage(t)

			_, err := localcerts.Resolve(localConfig("workshop.test", false))
			if err == nil {
				t.Fatal("Resolve accepted an unusable root CA")
			}
			if !strings.Contains(err.Error(), "educates-ca") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not name the root and mention %q:\n%v", tc.want, err)
			}
		})
	}
}

// stageMixedCache caches one Secret of every role: the root educates-ca,
// a CA key pair minted for other.test, a trust-only CA certificate, a TLS
// certificate, registry credentials, and a CA with its key in the shape
// alpha releases cached.
func stageMixedCache(t *testing.T) {
	t.Helper()
	root := testpki.NewRootCA()
	cacheRoot(t, "educates-ca", root)
	resolveAndCommit(t, localConfig("other.test", false))
	writeCached(t, secrets.NewCATrustSecret("corporate-ca", testpki.NewRootCA().CertPEM))
	leaf := root.NewLeaf(testpki.WithDNSNames("*.wildcard.test"))
	writeCached(t, secrets.NewTLSSecret("wildcard-tls", leaf.ChainPEM(), leaf.KeyPEM, ""))
	writeCached(t, secrets.NewDockerRegistrySecret("registry", "registry.test", "user", "pass", ""))
	alpha := testpki.NewRootCA()
	writeCached(t, secrets.NewTLSSecret("workshop.test-ca", alpha.CertPEM, alpha.KeyPEM, "workshop.test"))
}

func TestResolve_SyncsOnlyTheSelectedCAKeyPair(t *testing.T) {
	for name, tc := range map[string]struct {
		insecure bool
		want     []string
	}{
		"secure": {
			want: []string{"corporate-ca", "educates-ca", "registry", "wildcard-tls", "workshop.test-ca-key-pair"},
		},
		"insecure": {
			insecure: true,
			want:     []string{"corporate-ca", "educates-ca", "registry", "wildcard-tls"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			withDataHome(t)
			stageMixedCache(t)

			// The selected CA key pair is synced although only Commit
			// writes it to the cache.
			plan, err := localcerts.Resolve(localConfig("workshop.test", tc.insecure))
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if !reflect.DeepEqual(plan.SyncSecretNames, tc.want) {
				t.Errorf("plan syncs %v, want %v", plan.SyncSecretNames, tc.want)
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

func TestResolveSync_PushesOnlyTheCAKeyPairTheLocalConfigSelects(t *testing.T) {
	const secureConfig = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
ingress:
  domain: workshop.test
`
	const insecureConfig = secureConfig + "  insecure: true\n"
	withoutCAKeyPairs := []string{"corporate-ca", "educates-ca", "registry", "wildcard-tls"}

	for name, tc := range map[string]struct {
		config    string
		want      []string
		wantMints int
	}{
		"without a local config":        {want: withoutCAKeyPairs},
		"with an insecure local config": {config: insecureConfig, want: withoutCAKeyPairs},
		"with a secure local config": {
			config:    secureConfig,
			want:      []string{"corporate-ca", "educates-ca", "registry", "wildcard-tls", "workshop.test-ca-key-pair"},
			wantMints: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			dataHome := withDataHome(t)
			stageMixedCache(t)
			if tc.config != "" {
				if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(tc.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			plan, err := localcerts.ResolveSync()
			if err != nil {
				t.Fatalf("ResolveSync: %v", err)
			}
			if !reflect.DeepEqual(plan.SyncSecretNames, tc.want) {
				t.Errorf("sync pushes %v, want %v", plan.SyncSecretNames, tc.want)
			}
			var r noteRecorder
			if err := plan.Commit(&r); err != nil {
				t.Fatalf("Commit: %v", err)
			}
			if len(r.notes) != tc.wantMints {
				t.Errorf("Commit printed %q, want %d mint lines", r.notes, tc.wantMints)
			}
			for _, name := range plan.SyncSecretNames {
				if _, err := secrets.LoadCachedSecret(name); err != nil {
					t.Errorf("sync pushes %q, which the cache does not hold after Commit: %v", name, err)
				}
			}
		})
	}
}

func TestResolveSync_InvalidLocalConfig_Errors(t *testing.T) {
	dataHome := withDataHome(t)
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte("kind: [not yaml"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := localcerts.ResolveSync(); err == nil || !strings.Contains(err.Error(), "config.yaml") {
		t.Fatalf("ResolveSync error = %v, want one naming the local config", err)
	}
}

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

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
