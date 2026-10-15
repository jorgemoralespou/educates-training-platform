package cmd

import (
	"bytes"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"io"
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

// listColumns are the columns `educates local secrets list` prints.
var listColumns = []string{"NAME", "TYPE", "DOMAINS", "EXPIRES"}

// listCachedSecrets runs `educates local secrets list` and returns its
// rows keyed by NAME, each a map of column to cell.
func listCachedSecrets(t *testing.T) map[string]map[string]string {
	t.Helper()
	var out bytes.Buffer
	if err := (&LocalSecretsListOptions{}).Run(&out); err != nil {
		t.Fatalf("list: %v", err)
	}
	return parseListTable(t, out.String())
}

// parseListTable reads the list table: each column starts where its
// header starts.
func parseListTable(t *testing.T, table string) map[string]map[string]string {
	t.Helper()
	lines := strings.Split(strings.TrimRight(table, "\n"), "\n")
	header := lines[0]
	if got := strings.Fields(header); !reflect.DeepEqual(got, listColumns) {
		t.Fatalf("header %q, want columns %v", header, listColumns)
	}
	starts := make([]int, len(listColumns))
	from := 0
	for i, column := range listColumns {
		starts[i] = from + strings.Index(header[from:], column)
		from = starts[i] + len(column)
	}

	rows := map[string]map[string]string{}
	for _, line := range lines[1:] {
		row := map[string]string{}
		for i, column := range listColumns {
			start, end := starts[i], len(line)
			if i+1 < len(starts) && starts[i+1] < end {
				end = starts[i+1]
			}
			if start < end {
				row[column] = strings.TrimSpace(line[start:end])
			} else {
				row[column] = ""
			}
		}
		rows[row["NAME"]] = row
	}
	return rows
}

// assertListRow checks the row of name against want, which gives every
// column but NAME.
func assertListRow(t *testing.T, rows map[string]map[string]string, name string, want map[string]string) {
	t.Helper()
	got, ok := rows[name]
	if !ok {
		t.Errorf("no row for %q in the list", name)
		return
	}
	for _, column := range listColumns[1:] {
		if got[column] != want[column] {
			t.Errorf("%s %s = %q, want %q", name, column, got[column], want[column])
		}
	}
}

// validUntil is a validity window that ends on the given date, at noon
// UTC, so the expiry date reads the same in every time zone.
func validUntil(year int, month time.Month, day int) testpki.Option {
	return testpki.WithValidity(time.Now().Add(-time.Hour), time.Date(year, month, day, 12, 0, 0, 0, time.UTC))
}

func TestLocalSecretsList_CACertificate(t *testing.T) {
	withSecretsCache(t)
	ca := testpki.NewRootCA(testpki.WithCommonName("corporate root"), validUntil(2031, time.March, 4))
	if err := secrets.WriteCachedSecret(secrets.NewCATrustSecret("corporate-ca", ca.CertPEM)); err != nil {
		t.Fatal(err)
	}

	assertListRow(t, listCachedSecrets(t), "corporate-ca", map[string]string{
		"TYPE": "CA certificate", "DOMAINS": "any", "EXPIRES": "2031-03-04",
	})
}

// registerRoot caches root as the root CA name, as
// `educates local secrets add ca NAME --cert FILE --key FILE` does.
func registerRoot(t *testing.T, name string, root *testpki.Certificate) {
	t.Helper()
	files := writeFiles(t, root.CertPEM, root.KeyPEM)
	o := &LocalSecretsAddCaOptions{CertFile: files[0], KeyFile: files[1]}
	if err := o.Run(&bytes.Buffer{}, name); err != nil {
		t.Fatalf("add ca %s: %v", name, err)
	}
}

func TestLocalSecretsList_SuppliedRootShowsItsNameConstraints(t *testing.T) {
	cases := map[string]struct {
		root    *testpki.Certificate
		domains string
	}{
		"constrained": {
			root: testpki.NewRootCA(testpki.WithCommonName("team root"), validUntil(2030, time.June, 30),
				testpki.WithPermittedDNSDomains("workshops.test", ".lab.test")),
			domains: "workshops.test, .lab.test",
		},
		"unconstrained": {
			root:    testpki.NewRootCA(testpki.WithCommonName("team root"), validUntil(2030, time.June, 30)),
			domains: "any",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withSecretsCache(t)
			registerRoot(t, "team-ca", tc.root)

			rows := listCachedSecrets(t)
			assertListRow(t, rows, "team-ca", map[string]string{
				"TYPE": "CA certificate", "DOMAINS": tc.domains, "EXPIRES": "2030-06-30",
			})
			assertListRow(t, rows, "team-ca-root-key-pair", map[string]string{
				"TYPE": "CA root key pair", "DOMAINS": tc.domains, "EXPIRES": "2030-06-30",
			})
		})
	}
}

func TestLocalSecretsList_CAKeyPairShowsItsDomain(t *testing.T) {
	withSecretsCache(t)
	registerRoot(t, "team-ca", testpki.NewRootCA(testpki.WithCommonName("team root"), validUntil(2030, time.June, 30)))
	mintCAKeyPairs(t, "workshops.test")

	assertListRow(t, listCachedSecrets(t), "workshops.test-ca-key-pair", map[string]string{
		"TYPE": "CA intermediate key pair", "DOMAINS": "workshops.test", "EXPIRES": "2030-06-30",
	})
}

func TestLocalSecretsList_TLSCertificateShowsEveryWildcardDomain(t *testing.T) {
	withSecretsCache(t)
	// Like a mkcert certificate: no common name, and one wildcard DNS
	// name for each of several domains beside a plain one.
	leaf := testpki.NewRootCA().NewLeaf(
		func(tmpl *x509.Certificate) {
			tmpl.Subject = pkix.Name{Organization: []string{"mkcert development certificate"}, OrganizationalUnit: []string{"dev@laptop"}}
		},
		testpki.WithDNSNames("*.workshops.test", "workshops.test", "*.Labs.test."),
		validUntil(2028, time.January, 15),
	)
	files := writeFiles(t, leaf.CertPEM, leaf.KeyPEM)
	if err := (&LocalSecretsAddTlsOptions{CertFile: files[0], KeyFile: files[1]}).Run("dev-tls"); err != nil {
		t.Fatalf("add tls: %v", err)
	}

	assertListRow(t, listCachedSecrets(t), "dev-tls", map[string]string{
		"TYPE": "TLS certificate", "DOMAINS": "labs.test, workshops.test", "EXPIRES": "2028-01-15",
	})
}

func TestLocalSecretsList_SecretsWithoutCertificates(t *testing.T) {
	withSecretsCache(t)
	registry := &LocalSecretsAddDockerRegistryOptions{Server: "registry.test", Username: "user", Password: "pass"}
	if err := registry.Run("registry-credentials"); err != nil {
		t.Fatalf("add docker-registry: %v", err)
	}
	generic := &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "api-token"},
		Type:       apiv1.SecretTypeOpaque,
		StringData: map[string]string{"token": "s3cr3t"},
	}
	if err := secrets.WriteCachedSecret(generic); err != nil {
		t.Fatal(err)
	}

	rows := listCachedSecrets(t)
	assertListRow(t, rows, "registry-credentials", map[string]string{"TYPE": "Registry credentials"})
	assertListRow(t, rows, "api-token", map[string]string{"TYPE": "Generic"})
}

// cacheAlphaCA caches ca under name in the shape 4.0.0 alpha releases
// cached a CA: a kubernetes.io/tls Secret holding the CA certificate and
// its private key, annotated with the ingress domain it was for.
func cacheAlphaCA(t *testing.T, name string, ca *testpki.Certificate, domain string) {
	t.Helper()
	if err := secrets.WriteCachedSecret(withDomainAnnotation(secrets.NewTLSSecret(name, ca.CertPEM, ca.KeyPEM), domain)); err != nil {
		t.Fatal(err)
	}
}

func TestLocalSecretsList_AlphaCAListedAsInvalid(t *testing.T) {
	// Deploys neither use nor copy a CA cached by an alpha release, so it
	// is valid for no domain and lists as invalid, for the user to
	// migrate and remove. Its expiry still shows, and
	// `educates local secrets view` identifies it.
	withSecretsCache(t)
	cacheAlphaCA(t, "workshops-ca", testpki.NewRootCA(testpki.WithCommonName("alpha root"), validUntil(2034, time.May, 9)), "workshops.test")
	corporate := testpki.NewRootCA(testpki.WithCommonName("corporate root"), validUntil(2031, time.March, 4))
	if err := secrets.WriteCachedSecret(secrets.NewCATrustSecret("corporate-ca", corporate.CertPEM)); err != nil {
		t.Fatal(err)
	}

	rows := listCachedSecrets(t)
	assertListRow(t, rows, "workshops-ca", map[string]string{
		"TYPE": "invalid", "DOMAINS": "", "EXPIRES": "2034-05-09",
	})
	assertListRow(t, rows, "corporate-ca", map[string]string{
		"TYPE": "CA certificate", "DOMAINS": "any", "EXPIRES": "2031-03-04",
	})
}

// exportedTLSKey returns the private key the cached Secret name holds as
// tls.key, extracted from what `educates local secrets export NAME`
// prints the way the release notes extract it:
// awk '$1 == "tls.key:" {print $2}' | base64 --decode.
func exportedTLSKey(t *testing.T, name string) []byte {
	t.Helper()
	c := (&ProjectInfo{}).NewLocalSecretsExportCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(io.Discard)
	c.SilenceUsage = true
	c.SilenceErrors = true
	c.SetArgs([]string{name})
	if err := c.Execute(); err != nil {
		t.Fatalf("export %s: %v", name, err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[0] == "tls.key:" {
			key, err := base64.StdEncoding.DecodeString(fields[1])
			if err != nil {
				t.Fatalf("tls.key of %s does not decode: %v", name, err)
			}
			return key
		}
	}
	t.Fatalf("export of %s has no tls.key line:\n%s", name, out.String())
	return nil
}

func TestLocalSecretsList_AlphaCAReRegisteredAsTheRoot_ListsNoInvalidEntry(t *testing.T) {
	// The migration the release notes give for a CA cached by an alpha
	// release: register its certificate and key as the root CA under a
	// new name, then remove the old entry.
	withSecretsCache(t)
	alpha := testpki.NewRootCA(testpki.WithCommonName("alpha root"), validUntil(2034, time.May, 9))
	cacheAlphaCA(t, "workshops-ca", alpha, "workshops.test")

	certPEM, err := exportPEM(t, "workshops-ca")
	if err != nil {
		t.Fatalf("export --pem: %v", err)
	}
	files := writeFiles(t, []byte(certPEM), exportedTLSKey(t, "workshops-ca"))
	if err := (&LocalSecretsAddCaOptions{CertFile: files[0], KeyFile: files[1]}).Run(&bytes.Buffer{}, "educates-ca"); err != nil {
		t.Fatalf("add ca beside the alpha CA: %v", err)
	}
	if err := (&LocalSecretsRemoveOptions{}).Run(&bytes.Buffer{}, "workshops-ca"); err != nil {
		t.Fatalf("remove: %v", err)
	}

	rows := listCachedSecrets(t)
	if len(rows) != 2 {
		t.Errorf("list has %d rows, want the root CA's two", len(rows))
	}
	for _, name := range []string{"educates-ca", "educates-ca-root-key-pair"} {
		want := map[string]string{"TYPE": "CA certificate", "DOMAINS": "any", "EXPIRES": "2034-05-09"}
		if name == "educates-ca-root-key-pair" {
			want["TYPE"] = "CA root key pair"
		}
		assertListRow(t, rows, name, want)
	}
	if got := assertRootRegistered(t, "educates-ca"); !got.Equal(alpha.Cert) {
		t.Errorf("registered root %q, want the alpha CA %q", got.Subject, alpha.Cert.Subject)
	}
}

func TestLocalSecretsList_ExpiredCertificateShowsExpired(t *testing.T) {
	withSecretsCache(t)
	leaf := testpki.NewRootCA().NewLeaf(
		testpki.WithCommonName("old wildcard"),
		testpki.WithDNSNames("*.workshops.test"),
		testpki.WithValidity(time.Now().AddDate(-2, 0, 0), time.Now().AddDate(-1, 0, 0)),
	)
	if err := secrets.WriteCachedSecret(secrets.NewTLSSecret("old-tls", leaf.CertPEM, leaf.KeyPEM)); err != nil {
		t.Fatal(err)
	}

	assertListRow(t, listCachedSecrets(t), "old-tls", map[string]string{
		"TYPE": "TLS certificate", "DOMAINS": "workshops.test", "EXPIRES": "expired",
	})
}

func TestLocalSecretsList_MalformedFileListedAsInvalid(t *testing.T) {
	withSecretsCache(t)
	ca := testpki.NewRootCA(testpki.WithCommonName("corporate root"), validUntil(2031, time.March, 4))
	if err := secrets.WriteCachedSecret(secrets.NewCATrustSecret("corporate-ca", ca.CertPEM)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretsCacheDir(), "broken.yaml"), []byte("kind: Secret\ndata: [not a map\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	rows := listCachedSecrets(t)
	assertListRow(t, rows, "broken", map[string]string{"TYPE": "invalid"})
	assertListRow(t, rows, "corporate-ca", map[string]string{
		"TYPE": "CA certificate", "DOMAINS": "any", "EXPIRES": "2031-03-04",
	})
}

func TestLocalSecretsList_CacheDirectoryUnreadable_ReturnsError(t *testing.T) {
	withSecretsCache(t)
	// A file where the cache directory should be cannot be read as one.
	if err := os.WriteFile(secretsCacheDir(), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	err := (&LocalSecretsListOptions{}).Run(&out)
	if err == nil {
		t.Fatalf("list succeeded, printing:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "unable to list cached secrets") {
		t.Errorf("error %q does not say the cache could not be listed", err)
	}
	if out.Len() != 0 {
		t.Errorf("list printed output on error:\n%s", out.String())
	}
}
