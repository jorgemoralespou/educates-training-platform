package cmd

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

// runLocalSecretsView runs `educates local secrets view NAME` and returns
// what it printed.
func runLocalSecretsView(t *testing.T, name string) (string, error) {
	t.Helper()
	c := (&ProjectInfo{}).NewLocalSecretsCmdGroup()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(io.Discard)
	c.SilenceUsage = true
	c.SilenceErrors = true
	c.SetArgs([]string{"view", name})
	err := c.Execute()
	return out.String(), err
}

// secretView is what `educates local secrets view` printed: its labelled
// lines, and those of each certificate it describes.
type secretView struct {
	fieldBlock
	certificates []certificateView
}

// certificateView is the block of labelled lines describing one
// certificate, under its heading.
type certificateView struct {
	heading string
	fieldBlock
}

// fieldBlock is a run of "Label: value" lines.
type fieldBlock struct {
	// labels are the labels in the order they were printed.
	labels []string
	fields map[string]string
}

func (b *fieldBlock) add(t *testing.T, line string) {
	t.Helper()
	label, value, ok := strings.Cut(line, ":")
	if !ok {
		t.Fatalf("line %q is not a labelled line", line)
	}
	if b.fields == nil {
		b.fields = map[string]string{}
	}
	b.labels = append(b.labels, label)
	b.fields[label] = strings.TrimSpace(value)
}

// viewCachedSecret runs `educates local secrets view NAME` and parses
// what it printed: labelled lines, and certificate blocks, each a heading
// ending in a colon followed by indented labelled lines.
func viewCachedSecret(t *testing.T, name string) secretView {
	t.Helper()
	out, err := runLocalSecretsView(t, name)
	if err != nil {
		t.Fatalf("view %s: %v", name, err)
	}

	var view secretView
	var cert *certificateView
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.TrimSpace(line) == "":
			continue
		case strings.HasPrefix(line, "  "):
			if cert == nil {
				t.Fatalf("indented line %q outside a certificate block:\n%s", line, out)
			}
			cert.add(t, strings.TrimSpace(line))
		case strings.HasSuffix(line, ":") && !strings.Contains(line, ": "):
			view.certificates = append(view.certificates, certificateView{heading: strings.TrimSuffix(line, ":")})
			cert = &view.certificates[len(view.certificates)-1]
		default:
			cert = nil
			view.add(t, line)
		}
	}
	return view
}

// assertFields checks block has exactly the labels want gives, in that
// order, with their values.
func assertFields(t *testing.T, what string, block fieldBlock, want [][2]string) {
	t.Helper()
	var labels []string
	for _, field := range want {
		labels = append(labels, field[0])
		if got := block.fields[field[0]]; got != field[1] {
			t.Errorf("%s %s = %q, want %q", what, field[0], got, field[1])
		}
	}
	if !reflect.DeepEqual(block.labels, labels) {
		t.Errorf("%s labels = %q, want %q", what, block.labels, labels)
	}
}

// colonPairs is the hexadecimal digits digits in uppercase, as colon
// separated pairs.
func colonPairs(digits string) string {
	digits = strings.ToUpper(digits)
	if len(digits)%2 == 1 {
		digits = "0" + digits
	}
	var pairs []string
	for i := 0; i < len(digits); i += 2 {
		pairs = append(pairs, digits[i:i+2])
	}
	return strings.Join(pairs, ":")
}

// fingerprint is the SHA-256 fingerprint of cert as colon separated
// pairs of uppercase hexadecimal digits.
func fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return colonPairs(hex.EncodeToString(sum[:]))
}

// withSerial sets the serial number of the certificate.
func withSerial(serial int64) testpki.Option {
	return func(tmpl *x509.Certificate) {
		tmpl.SerialNumber = big.NewInt(serial)
	}
}

// validBetween is a validity window between two fixed instants, so the
// view of a certificate reads the same on every run.
func validBetween(notBefore, notAfter time.Time) testpki.Option {
	return testpki.WithValidity(notBefore, notAfter)
}

var (
	// validFrom is when the certificates of the view tests become valid.
	validFrom = time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)

	// validFromLine is validFrom as view prints it.
	validFromLine = "2026-01-02 03:04:05 UTC"
)

func TestLocalSecretsView_CACertificate(t *testing.T) {
	withSecretsCache(t)
	ca := testpki.NewRootCA(
		testpki.WithCommonName("corporate root"),
		validBetween(validFrom, time.Date(2031, time.March, 4, 12, 0, 0, 0, time.UTC)),
		testpki.WithPermittedDNSDomains("corp.test"),
		testpki.WithMaxPathLen(1),
		withSerial(0x1a2b3c),
	)
	if err := secrets.WriteCachedSecret(secrets.NewCATrustSecret("corporate-ca", ca.CertPEM)); err != nil {
		t.Fatal(err)
	}

	view := viewCachedSecret(t, "corporate-ca")

	assertFields(t, "view", view.fieldBlock, [][2]string{
		{"Name", "corporate-ca"},
		{"Type", "CA certificate"},
		{"Location", filepath.Join(secretsCacheDir(), "corporate-ca.yaml")},
		{"Secret type", "Opaque"},
		{"Domains", "corp.test"},
		{"Private key", "absent"},
	})
	if len(view.certificates) != 1 {
		t.Fatalf("view describes %d certificates, want 1", len(view.certificates))
	}
	cert := view.certificates[0]
	if cert.heading != "Certificate (ca.crt)" {
		t.Errorf("certificate heading %q, want %q", cert.heading, "Certificate (ca.crt)")
	}
	assertFields(t, "ca.crt", cert.fieldBlock, [][2]string{
		{"Common name", "corporate root"},
		{"Subject", "CN=corporate root"},
		{"Issuer", "CN=corporate root"},
		{"Valid from", validFromLine},
		{"Expires", "2031-03-04 12:00:00 UTC"},
		{"CA", "yes, path length 1"},
		{"Name constraints", "permitted: corp.test"},
		{"Serial", "1A:2B:3C"},
		{"SHA-256 fingerprint", fingerprint(ca.Cert)},
	})
}

// teamRoot is a root CA as a user supplies it with
// `educates local secrets add ca NAME --cert FILE --key FILE`.
func teamRoot() *testpki.Certificate {
	return testpki.NewRootCA(
		testpki.WithCommonName("team root"),
		validBetween(validFrom, time.Date(2030, time.June, 30, 12, 0, 0, 0, time.UTC)),
		withSerial(0x0102),
	)
}

// teamRootFields are the fields view prints for teamRoot.
func teamRootFields(root *testpki.Certificate) [][2]string {
	return [][2]string{
		{"Common name", "team root"},
		{"Subject", "CN=team root"},
		{"Issuer", "CN=team root"},
		{"Valid from", validFromLine},
		{"Expires", "2030-06-30 12:00:00 UTC"},
		{"CA", "yes, no path length limit"},
		{"Name constraints", "any"},
		{"Serial", "01:02"},
		{"SHA-256 fingerprint", fingerprint(root.Cert)},
	}
}

// assertHeadings checks the headings of the certificate blocks of view.
func assertHeadings(t *testing.T, view secretView, want ...string) {
	t.Helper()
	var got []string
	for _, cert := range view.certificates {
		got = append(got, cert.heading)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("certificate headings %q, want %q", got, want)
	}
}

func TestLocalSecretsView_CARootKeyPair(t *testing.T) {
	withSecretsCache(t)
	root := teamRoot()
	registerRoot(t, "team-ca", root)

	view := viewCachedSecret(t, "team-ca-root-key-pair")

	assertFields(t, "view", view.fieldBlock, [][2]string{
		{"Name", "team-ca-root-key-pair"},
		{"Type", "CA root key pair"},
		{"Location", filepath.Join(secretsCacheDir(), "team-ca-root-key-pair.yaml")},
		{"Secret type", "kubernetes.io/tls"},
		{"Domains", "any"},
		{"Private key", "present"},
	})
	assertHeadings(t, view, "Certificate (tls.crt)")
	assertFields(t, "tls.crt", view.certificates[0].fieldBlock, teamRootFields(root))
}

func TestLocalSecretsView_CAKeyPairShowsItsDomainAndRoot(t *testing.T) {
	withSecretsCache(t)
	root := teamRoot()
	registerRoot(t, "team-ca", root)
	mintCAKeyPairs(t, "workshops.test")
	minted := pemCertificates(t, cachedSecret(t, "workshops.test-ca-key-pair").Data["tls.crt"])[0]

	view := viewCachedSecret(t, "workshops.test-ca-key-pair")

	assertFields(t, "view", view.fieldBlock, [][2]string{
		{"Name", "workshops.test-ca-key-pair"},
		{"Type", "CA intermediate key pair"},
		{"Location", filepath.Join(secretsCacheDir(), "workshops.test-ca-key-pair.yaml")},
		{"Secret type", "kubernetes.io/tls"},
		{"Domains", "workshops.test"},
		{"Ingress domain", "workshops.test"},
		{"Root CA", "team-ca"},
		{"Private key", "present"},
	})
	assertHeadings(t, view, "Certificate (tls.crt, 1 of 2)", "Certificate (tls.crt, 2 of 2)", "Certificate (ca.crt)")
	assertFields(t, "minted CA", view.certificates[0].fieldBlock, [][2]string{
		{"Common name", "workshops.test"},
		{"Subject", "CN=workshops.test"},
		{"Issuer", "CN=team root"},
		{"Valid from", minted.NotBefore.UTC().Format("2006-01-02 15:04:05 UTC")},
		{"Expires", "2030-06-30 12:00:00 UTC"},
		{"CA", "yes, path length 0"},
		{"Name constraints", "permitted: workshops.test; excluded: 0.0.0.0/0, ::/0"},
		{"Serial", colonPairs(minted.SerialNumber.Text(16))},
		{"SHA-256 fingerprint", fingerprint(minted)},
	})
	assertFields(t, "root in tls.crt", view.certificates[1].fieldBlock, teamRootFields(root))
	assertFields(t, "root in ca.crt", view.certificates[2].fieldBlock, teamRootFields(root))
}

func TestLocalSecretsView_TLSCertificate(t *testing.T) {
	withSecretsCache(t)
	// Like a mkcert certificate: no common name, a wildcard and a plain
	// DNS name, and the loopback addresses.
	leaf := testpki.NewRootCA(testpki.WithCommonName("mkcert root")).NewLeaf(
		func(tmpl *x509.Certificate) {
			tmpl.Subject = pkix.Name{Organization: []string{"mkcert development certificate"}, OrganizationalUnit: []string{"dev@laptop"}}
		},
		testpki.WithDNSNames("*.workshops.test", "workshops.test"),
		testpki.WithIPAddresses(net.ParseIP("127.0.0.1"), net.ParseIP("::1")),
		validBetween(validFrom, time.Date(2028, time.January, 15, 12, 0, 0, 0, time.UTC)),
		withSerial(0xabcdef),
	)
	files := writeFiles(t, leaf.CertPEM, leaf.KeyPEM)
	if err := (&LocalSecretsAddTlsOptions{CertFile: files[0], KeyFile: files[1]}).Run("dev-tls"); err != nil {
		t.Fatalf("add tls: %v", err)
	}

	view := viewCachedSecret(t, "dev-tls")

	assertFields(t, "view", view.fieldBlock, [][2]string{
		{"Name", "dev-tls"},
		{"Type", "TLS certificate"},
		{"Location", filepath.Join(secretsCacheDir(), "dev-tls.yaml")},
		{"Secret type", "kubernetes.io/tls"},
		{"Domains", "workshops.test"},
		{"Private key", "present"},
	})
	assertHeadings(t, view, "Certificate (tls.crt)")
	assertFields(t, "tls.crt", view.certificates[0].fieldBlock, [][2]string{
		{"Subject", "OU=dev@laptop,O=mkcert development certificate"},
		{"Issuer", "CN=mkcert root"},
		{"SANs", "*.workshops.test, workshops.test, 127.0.0.1, ::1"},
		{"Valid from", validFromLine},
		{"Expires", "2028-01-15 12:00:00 UTC"},
		{"CA", "no"},
		{"Serial", "AB:CD:EF"},
		{"SHA-256 fingerprint", fingerprint(leaf.Cert)},
	})
}

func TestLocalSecretsView_ExpiredCertificateShowsExpired(t *testing.T) {
	withSecretsCache(t)
	leaf := testpki.NewRootCA().NewLeaf(
		testpki.WithDNSNames("*.workshops.test"),
		validBetween(time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, time.March, 1, 0, 0, 0, 0, time.UTC)),
	)
	if err := secrets.WriteCachedSecret(secrets.NewTLSSecret("old-tls", leaf.CertPEM, leaf.KeyPEM)); err != nil {
		t.Fatal(err)
	}

	view := viewCachedSecret(t, "old-tls")

	assertHeadings(t, view, "Certificate (tls.crt)")
	if got, want := view.certificates[0].fields["Expires"], "2025-03-01 00:00:00 UTC (expired)"; got != want {
		t.Errorf("Expires = %q, want %q", got, want)
	}
}

func TestLocalSecretsView_RegistryCredentialsShowTheirRegistries(t *testing.T) {
	withSecretsCache(t)
	credentials := secrets.NewDockerRegistrySecret("registry-credentials", "registry.test", "user", "pass", "user@example.test")
	// A second registry in the same Secret, as a hand-written
	// .dockerconfigjson may hold.
	credentials.Data[apiv1.DockerConfigJsonKey] = []byte(`{"auths":{"registry.test":{"auth":"dXNlcjpwYXNz"},"ghcr.io":{"username":"other","password":"secret"}}}`)
	if err := secrets.WriteCachedSecret(credentials); err != nil {
		t.Fatal(err)
	}

	view := viewCachedSecret(t, "registry-credentials")

	assertFields(t, "view", view.fieldBlock, [][2]string{
		{"Name", "registry-credentials"},
		{"Type", "Registry credentials"},
		{"Location", filepath.Join(secretsCacheDir(), "registry-credentials.yaml")},
		{"Secret type", "kubernetes.io/dockerconfigjson"},
		{"Registries", "ghcr.io, registry.test"},
	})
	assertHeadings(t, view)
}

func TestLocalSecretsView_GenericShowsTheNamesOfItsKeys(t *testing.T) {
	withSecretsCache(t)
	generic := &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "api-token"},
		Type:       apiv1.SecretTypeOpaque,
		Data:       map[string][]byte{"token": []byte("s3cr3t")},
		StringData: map[string]string{"endpoint": "https://api.test"},
	}
	if err := secrets.WriteCachedSecret(generic); err != nil {
		t.Fatal(err)
	}

	view := viewCachedSecret(t, "api-token")

	assertFields(t, "view", view.fieldBlock, [][2]string{
		{"Name", "api-token"},
		{"Type", "Generic"},
		{"Location", filepath.Join(secretsCacheDir(), "api-token.yaml")},
		{"Secret type", "Opaque"},
		{"Keys", "endpoint, token"},
	})
	assertHeadings(t, view)
}

func TestLocalSecretsView_UnreadableFileShowsWhyItIsInvalid(t *testing.T) {
	withSecretsCache(t)
	path := filepath.Join(secretsCacheDir(), "broken.yaml")
	if err := os.MkdirAll(secretsCacheDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("kind: Secret\ndata: [not a map\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	view := viewCachedSecret(t, "broken")

	reason := view.fields["Invalid because"]
	if want := fmt.Sprintf("unable to read secret file %q: ", path); !strings.HasPrefix(reason, want) {
		t.Errorf("Invalid because = %q, want it to start with %q", reason, want)
	}
	assertFields(t, "view", view.fieldBlock, [][2]string{
		{"Name", "broken"},
		{"Type", "invalid"},
		{"Location", path},
		{"Invalid because", reason},
		{"Remove with", "educates local secrets remove broken"},
	})
	assertHeadings(t, view)
}

func TestLocalSecretsView_AlphaCAShowsWhyItIsInvalid(t *testing.T) {
	withSecretsCache(t)
	alpha := teamRoot()
	cacheAlphaCA(t, "workshops-ca", alpha, "workshops.test")

	view := viewCachedSecret(t, "workshops-ca")

	reason := view.fields["Invalid because"]
	if want := `cached secret "workshops-ca" holds a CA certificate with its private key`; !strings.HasPrefix(reason, want) {
		t.Errorf("Invalid because = %q, want it to start with %q", reason, want)
	}
	if !strings.Contains(reason, "4.0.0 alpha releases") {
		t.Errorf("Invalid because = %q does not say alpha releases cached it", reason)
	}
	// Removing the entry removes the CA's key, so the view says how to keep
	// the CA, as the documented migration does, before how to remove it.
	assertFields(t, "view", view.fieldBlock, [][2]string{
		{"Name", "workshops-ca"},
		{"Type", "invalid"},
		{"Location", filepath.Join(secretsCacheDir(), "workshops-ca.yaml")},
		{"Secret type", "kubernetes.io/tls"},
		{"Private key", "present"},
		{"Invalid because", reason},
		{"To keep the CA", `register it as the root CA under a name other than workshops-ca before removing it, as "CAs cached by alpha releases" in the Local Environment documentation describes`},
		{"Register with", "educates local secrets add ca ROOT --cert FILE --key FILE"},
		{"Remove with", "educates local secrets remove workshops-ca"},
	})
	assertHeadings(t, view, "Certificate (tls.crt)")
	assertFields(t, "tls.crt", view.certificates[0].fieldBlock, teamRootFields(alpha))
}

// secretMaterial returns what no view of the Secret may print: the
// value of each of its keys, as it is and base64 encoded, and each line
// of it, which for PEM data is the base64 encoded key or certificate.
func secretMaterial(s *apiv1.Secret) []string {
	values := map[string][]byte{}
	for key, value := range s.Data {
		values[key] = value
	}
	for key, value := range s.StringData {
		values[key] = []byte(value)
	}
	var material []string
	for _, value := range values {
		material = append(material, string(value), base64.StdEncoding.EncodeToString(value))
		for _, line := range strings.Split(string(value), "\n") {
			if len(line) > 16 {
				material = append(material, line)
			}
		}
	}
	return material
}

func TestLocalSecretsView_PrintsNoKeyMaterial(t *testing.T) {
	withSecretsCache(t)
	root := teamRoot()
	registerRoot(t, "team-ca", root)
	mintCAKeyPairs(t, "workshops.test")
	leaf := root.NewLeaf(testpki.WithDNSNames("*.labs.test"))
	cacheAlphaCA(t, "alpha-ca", testpki.NewRootCA(), "alpha.test")
	genericKey := testpki.NewRootCA()
	for _, s := range []*apiv1.Secret{
		secrets.NewTLSSecret("labs-tls", leaf.CertPEM, leaf.KeyPEM),
		secrets.NewDockerRegistrySecret("registry-credentials", "registry.test", "registry-user", "registry-p4ssw0rd", "user@example.test"),
		{
			ObjectMeta: metav1.ObjectMeta{Name: "signing-key"},
			Type:       apiv1.SecretTypeOpaque,
			Data:       map[string][]byte{"key.pem": genericKey.KeyPEM, "token": []byte("generic-t0ken-value")},
		},
	} {
		if err := secrets.WriteCachedSecret(s); err != nil {
			t.Fatal(err)
		}
	}
	// A file that does not decode, holding a private key.
	unreadableKey := testpki.NewRootCA()
	broken := "kind: Secret\ndata:\n  tls.key: " + base64.StdEncoding.EncodeToString(unreadableKey.KeyPEM) + "\n  token: [unterminated\n"
	if err := os.WriteFile(filepath.Join(secretsCacheDir(), "broken.yaml"), []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}

	forbidden := []string{
		"PRIVATE KEY",
		"registry-user", "registry-p4ssw0rd",
		base64.StdEncoding.EncodeToString([]byte("registry-user:registry-p4ssw0rd")),
		"generic-t0ken-value",
	}
	forbidden = append(forbidden, secretMaterial(&apiv1.Secret{Data: map[string][]byte{"tls.key": unreadableKey.KeyPEM}})...)
	for _, name := range cachedSecretNames(t) {
		if s, err := secrets.LoadCachedSecret(name); err == nil {
			forbidden = append(forbidden, secretMaterial(s)...)
		}
	}

	roles := map[string]bool{}
	for _, name := range cachedSecretNames(t) {
		out, err := runLocalSecretsView(t, name)
		if err != nil {
			t.Fatalf("view %s: %v", name, err)
		}
		roles[viewCachedSecret(t, name).fields["Type"]] = true
		for _, material := range forbidden {
			if strings.Contains(out, material) {
				t.Errorf("view of %s prints %q:\n%s", name, material, out)
			}
		}
	}

	want := map[string]bool{}
	for _, role := range []secrets.CachedSecretRole{
		secrets.CACertificateRole, secrets.CARootKeyPairRole, secrets.CAKeyPairRole, secrets.TLSCertificateRole,
		secrets.RegistryCredentialsRole, secrets.GenericRole, secrets.InvalidRole,
	} {
		want[string(role)] = true
	}
	if !reflect.DeepEqual(roles, want) {
		t.Errorf("viewed roles %v, want every role %v", roles, want)
	}
}

func TestLocalSecretsView_UnknownName_Fails(t *testing.T) {
	withSecretsCache(t)
	if err := secrets.WriteCachedSecret(secrets.NewCATrustSecret("corporate-ca", testpki.NewRootCA().CertPEM)); err != nil {
		t.Fatal(err)
	}

	out, err := runLocalSecretsView(t, "missing-ca")
	if err == nil {
		t.Fatalf("view of an unknown name succeeded, printing:\n%s", out)
	}
	if want := `no secret named "missing-ca" in the local secrets cache`; err.Error() != want {
		t.Errorf("error %q, want %q", err, want)
	}
	if out != "" {
		t.Errorf("view printed output on error:\n%s", out)
	}
}
