package cmd

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	apiv1 "k8s.io/api/core/v1"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

// withSecretsCache points the CLI data home, and so the local secrets
// cache, at a fresh temporary directory.
func withSecretsCache(t *testing.T) {
	t.Helper()
	t.Setenv("EDUCATES_CLI_DATA_HOME", t.TempDir())
}

// withDomainAnnotation records domain on s with the domain annotation, as
// caches from earlier releases do, and returns s.
func withDomainAnnotation(s *apiv1.Secret, domain string) *apiv1.Secret {
	if s.Annotations == nil {
		s.Annotations = map[string]string{}
	}
	s.Annotations[secrets.DomainAnnotation] = domain
	return s
}

// cachedSecretNames lists the local secrets cache.
func cachedSecretNames(t *testing.T) []string {
	t.Helper()
	names, err := secrets.ListCachedSecretNames()
	if err != nil {
		t.Fatal(err)
	}
	return names
}

// cachedSecret loads a Secret from the local secrets cache.
func cachedSecret(t *testing.T, name string) *apiv1.Secret {
	t.Helper()
	s, err := secrets.LoadCachedSecret(name)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// pemCertificates parses every CERTIFICATE block of data.
func pemCertificates(t *testing.T, data []byte) []*x509.Certificate {
	t.Helper()
	var certs []*x509.Certificate
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			t.Fatalf("unexpected PEM block %q", block.Type)
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		t.Fatal("no certificate in PEM data")
	}
	return certs
}

// writeFiles writes each PEM blob to a temporary file and returns the
// paths in order.
func writeFiles(t *testing.T, blobs ...[]byte) []string {
	t.Helper()
	dir := t.TempDir()
	var paths []string
	for i, blob := range blobs {
		p := filepath.Join(dir, "file"+string(rune('a'+i))+".pem")
		if err := os.WriteFile(p, blob, 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	return paths
}

// assertRootRegistered checks the cache holds root CA name: the CA trust
// Secret name with only the root certificate as ca.crt, and the CA root
// key pair name-root-key-pair holding the same certificate and its key.
// It returns the root certificate.
func assertRootRegistered(t *testing.T, name string) *x509.Certificate {
	t.Helper()
	trust := cachedSecret(t, name)
	if trust.Type != apiv1.SecretTypeOpaque {
		t.Errorf("CA trust Secret type = %q, want Opaque", trust.Type)
	}
	if keys := secretKeys(trust); !reflect.DeepEqual(keys, []string{"ca.crt"}) {
		t.Errorf("CA trust Secret holds %v, want only ca.crt", keys)
	}
	trustCerts := pemCertificates(t, trust.Data["ca.crt"])
	if len(trustCerts) != 1 || !trustCerts[0].IsCA {
		t.Fatalf("CA trust Secret ca.crt holds %d certificates (first IsCA=%v), want one CA", len(trustCerts), trustCerts[0].IsCA)
	}
	root := trustCerts[0]

	keyPair := cachedSecret(t, name+"-root-key-pair")
	if keyPair.Type != apiv1.SecretTypeTLS {
		t.Errorf("CA root key pair type = %q, want kubernetes.io/tls", keyPair.Type)
	}
	if keys := secretKeys(keyPair); !reflect.DeepEqual(keys, []string{"tls.crt", "tls.key"}) {
		t.Errorf("CA root key pair holds %v, want tls.crt and tls.key", keys)
	}
	if got := pemCertificates(t, keyPair.Data["tls.crt"]); len(got) != 1 || !got[0].Equal(root) {
		t.Errorf("CA root key pair tls.crt is not the root certificate of the CA trust Secret")
	}
	if !keyMatches(t, root, keyPair.Data["tls.key"]) {
		t.Errorf("CA root key pair tls.key does not match the root certificate")
	}
	return root
}

func secretKeys(s *apiv1.Secret) []string {
	var keys []string
	for k := range s.Data {
		keys = append(keys, k)
	}
	for k := range s.StringData {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// keyMatches reports whether the PEM private key keyPEM is the key of
// cert, by signing with it and verifying with the certificate.
func keyMatches(t *testing.T, cert *x509.Certificate, keyPEM []byte) bool {
	t.Helper()
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		t.Fatal("no PEM block in key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse PKCS #8 key: %v", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		t.Fatalf("key %T is not a signer", key)
	}
	eq, ok := cert.PublicKey.(interface{ Equal(crypto.PublicKey) bool })
	return ok && eq.Equal(signer.Public())
}

func TestLocalSecretsAddCa_Generate_RegistersAnUnconstrainedRoot(t *testing.T) {
	withSecretsCache(t)

	var out bytes.Buffer
	o := &LocalSecretsAddCaOptions{}
	if err := o.Run(&out, "educates-ca"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got, want := cachedSecretNames(t), []string{"educates-ca", "educates-ca-root-key-pair"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cache holds %v, want %v", got, want)
	}
	root := assertRootRegistered(t, "educates-ca")
	if root.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("generated root cannot sign certificates")
	}
	if len(root.PermittedDNSDomains)+len(root.ExcludedDNSDomains)+len(root.PermittedIPRanges)+len(root.ExcludedIPRanges) != 0 {
		t.Errorf("generated root carries name constraints: %+v", root)
	}
	if years := root.NotAfter.Sub(root.NotBefore).Hours() / 24 / 365; years < 9.9 || years > 10.1 {
		t.Errorf("generated root is valid for %.1f years, want 10", years)
	}

	// The trust import instructions name the export command.
	for _, want := range []string{
		`Generated root CA "educates-ca"`,
		"educates local secrets export educates-ca --pem > educates-ca.pem",
		"trust store",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output does not mention %q:\n%s", want, out.String())
		}
	}
}

func TestLocalSecretsAddCa_Generate_InstructionsExportTheRootCertificate(t *testing.T) {
	withSecretsCache(t)
	if err := (&LocalSecretsAddCaOptions{}).Run(&bytes.Buffer{}, "educates-ca"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	root := assertRootRegistered(t, "educates-ca")

	// `educates local secrets export educates-ca --pem`
	var out bytes.Buffer
	if err := printSecretCertificatePEM(&out, "educates-ca"); err != nil {
		t.Fatalf("export --pem: %v", err)
	}
	if got := pemCertificates(t, out.Bytes()); len(got) != 1 || !got[0].Equal(root) {
		t.Errorf("export --pem printed %d certificates, want the root certificate", len(got))
	}
}

func TestLocalSecretsAddCa_CertAndKey_RegistersTheSuppliedRoot(t *testing.T) {
	withSecretsCache(t)
	supplied := testpki.NewRootCA(testpki.WithCommonName("mkcert development CA"))
	files := writeFiles(t, supplied.CertPEM, supplied.KeyPEM)

	var out bytes.Buffer
	o := &LocalSecretsAddCaOptions{CertFile: files[0], KeyFile: files[1]}
	if err := o.Run(&out, "mkcert-ca"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got, want := cachedSecretNames(t), []string{"mkcert-ca", "mkcert-ca-root-key-pair"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cache holds %v, want %v", got, want)
	}
	if root := assertRootRegistered(t, "mkcert-ca"); !root.Equal(supplied.Cert) {
		t.Errorf("registered root %q, want the supplied %q", root.Subject, supplied.Cert.Subject)
	}
	if strings.Contains(out.String(), "export") {
		t.Errorf("registering a supplied root printed trust import instructions:\n%s", out.String())
	}
}

func TestLocalSecretsAddCa_CertAlone_CachesOnlyTheCACertificate(t *testing.T) {
	withSecretsCache(t)
	ca := testpki.NewRootCA()
	files := writeFiles(t, ca.CertPEM)

	o := &LocalSecretsAddCaOptions{CertFile: files[0]}
	if err := o.Run(&bytes.Buffer{}, "corporate-ca"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got, want := cachedSecretNames(t), []string{"corporate-ca"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cache holds %v, want %v", got, want)
	}
	trust := cachedSecret(t, "corporate-ca")
	if trust.Type != apiv1.SecretTypeOpaque || !reflect.DeepEqual(secretKeys(trust), []string{"ca.crt"}) {
		t.Errorf("CA trust Secret is %q with %v, want Opaque with only ca.crt", trust.Type, secretKeys(trust))
	}
	if got := pemCertificates(t, trust.Data["ca.crt"]); len(got) != 1 || !got[0].Equal(ca.Cert) {
		t.Errorf("ca.crt is not the supplied CA certificate")
	}
}

func TestLocalSecretsAddCa_RejectsUnusableMaterial(t *testing.T) {
	ca := testpki.NewRootCA()
	other := testpki.NewRootCA()
	leaf := ca.NewLeaf(testpki.WithDNSNames("*.workshop.test"))

	cases := map[string]struct {
		cert, key []byte
		want      string
	}{
		"certificate that is not a CA":            {cert: leaf.CertPEM, key: leaf.KeyPEM, want: "is not a CA certificate"},
		"trust-only certificate that is not a CA": {cert: leaf.CertPEM, want: "is not a CA certificate"},
		"key that does not match the certificate": {cert: ca.CertPEM, key: other.KeyPEM, want: "does not match"},
		"file without a certificate":              {cert: ca.KeyPEM, key: ca.KeyPEM, want: "no PEM-encoded certificate"},
		"key file without a key":                  {cert: ca.CertPEM, key: ca.CertPEM, want: "no PEM-encoded private key"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withSecretsCache(t)
			o := &LocalSecretsAddCaOptions{CertFile: writeFiles(t, tc.cert)[0]}
			if tc.key != nil {
				o.KeyFile = writeFiles(t, tc.key)[0]
			}
			err := o.Run(&bytes.Buffer{}, "test-ca")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run error = %v, want one mentioning %q", err, tc.want)
			}
			if names := cachedSecretNames(t); len(names) != 0 {
				t.Errorf("a rejected CA left %v in the cache", names)
			}
		})
	}
}

func TestLocalSecretsAddCa_KeyWithoutCert_Errors(t *testing.T) {
	withSecretsCache(t)
	o := &LocalSecretsAddCaOptions{KeyFile: writeFiles(t, testpki.NewRootCA().KeyPEM)[0]}
	err := o.Run(&bytes.Buffer{}, "test-ca")
	if err == nil || !strings.Contains(err.Error(), "--key needs --cert") {
		t.Fatalf("Run error = %v, want one saying --key needs --cert", err)
	}
}

func TestLocalSecretsAddCa_ReservedSuffix_Errors(t *testing.T) {
	for _, name := range []string{"educates-root-key-pair", "workshop.test-ca-key-pair"} {
		t.Run(name, func(t *testing.T) {
			withSecretsCache(t)
			err := (&LocalSecretsAddCaOptions{}).Run(&bytes.Buffer{}, name)
			if err == nil || !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("Run error = %v, want a reserved-name error", err)
			}
			if names := cachedSecretNames(t); len(names) != 0 {
				t.Errorf("a refused CA left %v in the cache", names)
			}
		})
	}
}

func TestLocalSecretsAddCa_SecondRoot_RefusedNamingTheExistingOne(t *testing.T) {
	supplied := testpki.NewRootCA()
	files := writeFiles(t, supplied.CertPEM, supplied.KeyPEM)

	for name, second := range map[string]*LocalSecretsAddCaOptions{
		"generated":           {},
		"supplied with a key": {CertFile: files[0], KeyFile: files[1]},
	} {
		t.Run(name, func(t *testing.T) {
			withSecretsCache(t)
			if err := (&LocalSecretsAddCaOptions{}).Run(&bytes.Buffer{}, "educates-ca"); err != nil {
				t.Fatalf("first root: %v", err)
			}
			before := cachedSecret(t, "educates-ca")

			for _, newName := range []string{"another-ca", "educates-ca"} {
				err := second.Run(&bytes.Buffer{}, newName)
				if err == nil {
					t.Fatalf("a second root %q was registered", newName)
				}
				if !strings.Contains(err.Error(), `already registered in the local secrets cache: "educates-ca"`) {
					t.Errorf("error does not name the existing root:\n%v", err)
				}
			}
			if got, want := cachedSecretNames(t), []string{"educates-ca", "educates-ca-root-key-pair"}; !reflect.DeepEqual(got, want) {
				t.Errorf("cache holds %v, want %v", got, want)
			}
			if after := cachedSecret(t, "educates-ca"); !bytes.Equal(after.Data["ca.crt"], before.Data["ca.crt"]) {
				t.Error("the refused root replaced the existing root certificate")
			}
		})
	}
}

func TestLocalSecretsAddCa_CertAlone_AllowedBesideARootButNotOverIt(t *testing.T) {
	withSecretsCache(t)
	if err := (&LocalSecretsAddCaOptions{}).Run(&bytes.Buffer{}, "educates-ca"); err != nil {
		t.Fatalf("root: %v", err)
	}
	trustOnly := &LocalSecretsAddCaOptions{CertFile: writeFiles(t, testpki.NewRootCA().CertPEM)[0]}

	if err := trustOnly.Run(&bytes.Buffer{}, "corporate-ca"); err != nil {
		t.Fatalf("trust-only CA beside a root: %v", err)
	}
	err := trustOnly.Run(&bytes.Buffer{}, "educates-ca")
	if err == nil || !strings.Contains(err.Error(), "is the root CA") {
		t.Fatalf("Run error = %v, want a refusal to replace the root certificate", err)
	}
	assertRootRegistered(t, "educates-ca")
}
