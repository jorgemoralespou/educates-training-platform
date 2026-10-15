package cmd

import (
	"bytes"
	"crypto/x509"
	"io"
	"strings"
	"testing"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

// exportPEM runs `educates local secrets export NAME --pem` and returns
// what it printed.
func exportPEM(t *testing.T, name string) (string, error) {
	t.Helper()
	c := (&ProjectInfo{}).NewLocalSecretsExportCmd()
	var out bytes.Buffer
	c.SetOut(&out)
	c.SetErr(io.Discard)
	c.SilenceUsage = true
	c.SilenceErrors = true
	c.SetArgs([]string{name, "--pem"})
	err := c.Execute()
	return out.String(), err
}

func TestLocalSecretsExportPEM_PrintsCertificatesOnly(t *testing.T) {
	root := testpki.NewRootCA(testpki.WithCommonName("root"))
	intermediate := root.NewIntermediateCA(testpki.WithCommonName("intermediate"))
	leaf := intermediate.NewLeaf(testpki.WithCommonName("leaf"), testpki.WithDNSNames("*.workshop.test"))
	alpha := testpki.NewRootCA(testpki.WithCommonName("alpha"))

	combined := append(append([]byte{}, leaf.CertPEM...), leaf.KeyPEM...)

	cases := map[string]struct {
		secret *apiv1.Secret
		want   []*x509.Certificate
	}{
		"CA trust Secret prints its CA certificate": {
			secret: secrets.NewCATrustSecret("educates-ca", root.CertPEM),
			want:   []*x509.Certificate{root.Cert},
		},
		"CA root key pair prints the root certificate": {
			secret: secrets.NewCARootKeyPairSecret("educates-ca", root.CertPEM, root.KeyPEM),
			want:   []*x509.Certificate{root.Cert},
		},
		"CA key pair prints its root certificate": {
			secret: secrets.NewCAKeyPairSecret("workshop.test", "educates-ca", intermediate.CertPEM, intermediate.KeyPEM, root.CertPEM),
			want:   []*x509.Certificate{root.Cert},
		},
		"CA cached by an alpha release prints the CA certificate": {
			secret: secrets.NewTLSSecret("workshop.test-ca", alpha.CertPEM, alpha.KeyPEM, "workshop.test"),
			want:   []*x509.Certificate{alpha.Cert},
		},
		"TLS certificate prints its certificate chain": {
			secret: secrets.NewTLSSecret("wildcard-tls", leaf.ChainPEM(), leaf.KeyPEM, ""),
			want:   []*x509.Certificate{leaf.Cert, intermediate.Cert, root.Cert},
		},
		"TLS certificate with its key in tls.crt prints only the certificate": {
			secret: secrets.NewTLSSecret("combined-tls", combined, leaf.KeyPEM, ""),
			want:   []*x509.Certificate{leaf.Cert},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withSecretsCache(t)
			if err := secrets.WriteCachedSecret(tc.secret); err != nil {
				t.Fatal(err)
			}

			out, err := exportPEM(t, tc.secret.Name)
			if err != nil {
				t.Fatalf("export --pem: %v", err)
			}
			if strings.Contains(out, "PRIVATE KEY") {
				t.Fatalf("export --pem printed key material:\n%s", out)
			}
			got := pemCertificates(t, []byte(out))
			if len(got) != len(tc.want) {
				t.Fatalf("export --pem printed %d certificates, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if !got[i].Equal(tc.want[i]) {
					t.Errorf("certificate %d is %q, want %q", i, got[i].Subject, tc.want[i].Subject)
				}
			}
		})
	}
}

func TestLocalSecretsExportPEM_SecretWithoutCertificate_ErrorsPrintingNothing(t *testing.T) {
	key := testpki.NewRootCA().KeyPEM
	for name, secret := range map[string]*apiv1.Secret{
		"registry credentials": secrets.NewDockerRegistrySecret("registry", "registry.test", "user", "pass", ""),
		"generic secret holding a key": {
			ObjectMeta: metav1.ObjectMeta{Name: "generic"},
			Type:       apiv1.SecretTypeOpaque,
			Data:       map[string][]byte{"key.pem": key, "ca.crt": key},
		},
		"TLS secret holding a key as tls.crt": secrets.NewTLSSecret("broken-tls", key, key, ""),
	} {
		t.Run(name, func(t *testing.T) {
			withSecretsCache(t)
			if err := secrets.WriteCachedSecret(secret); err != nil {
				t.Fatal(err)
			}

			out, err := exportPEM(t, secret.Name)
			if err == nil || !strings.Contains(err.Error(), "does not contain a certificate") {
				t.Errorf("export --pem error = %v, want one saying the secret has no certificate", err)
			}
			if out != "" {
				t.Errorf("export --pem printed:\n%s", out)
			}
		})
	}
}
