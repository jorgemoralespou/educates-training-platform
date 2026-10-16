package cmd

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	apiv1 "k8s.io/api/core/v1"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
)

func TestLocalSecretsAddTls_CachesTheWildcardCertificateWithItsChain(t *testing.T) {
	withSecretsCache(t)
	intermediate := testpki.NewRootCA().NewIntermediateCA()
	leaf := intermediate.NewLeaf(testpki.WithDNSNames("*.workshop.test", "*.other.test", "workshop.test"))
	chain := append(append([]byte{}, leaf.CertPEM...), intermediate.CertPEM...)
	files := writeFiles(t, chain, leaf.KeyPEM)

	o := &LocalSecretsAddTlsOptions{CertFile: files[0], KeyFile: files[1]}
	if err := o.Run("workshop-tls"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got, want := cachedSecretNames(t), []string{"workshop-tls"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cache holds %v, want %v", got, want)
	}
	s := cachedSecret(t, "workshop-tls")
	if s.Type != apiv1.SecretTypeTLS {
		t.Errorf("type = %q, want kubernetes.io/tls", s.Type)
	}
	if keys := secretKeys(s); !reflect.DeepEqual(keys, []string{"tls.crt", "tls.key"}) {
		t.Errorf("Secret holds %v, want tls.crt and tls.key", keys)
	}
	got := pemCertificates(t, s.Data["tls.crt"])
	if len(got) != 2 || !got[0].Equal(leaf.Cert) || !got[1].Equal(intermediate.Cert) {
		t.Errorf("tls.crt holds %d certificates, want the certificate followed by its intermediate", len(got))
	}
	if !keyMatches(t, leaf.Cert, s.Data["tls.key"]) {
		t.Error("tls.key does not match the certificate")
	}
	// The domains a TLS certificate serves are its wildcard SANs, never
	// an annotation.
	if len(s.Annotations) != 0 {
		t.Errorf("annotations = %v, want none", s.Annotations)
	}
}

func TestLocalSecretsAddTls_RejectsUnusableMaterial(t *testing.T) {
	root := testpki.NewRootCA()
	wildcard := root.NewLeaf(testpki.WithDNSNames("*.workshop.test"))
	other := root.NewLeaf(testpki.WithDNSNames("*.workshop.test"))
	bare := root.NewLeaf(testpki.WithDNSNames("workshop.test", "portal.workshop.test"))

	cases := map[string]struct {
		cert, key []byte
		want      string
	}{
		"no --cert":                        {key: wildcard.KeyPEM, want: "--cert and --key"},
		"no --key":                         {cert: wildcard.CertPEM, want: "--cert and --key"},
		"CA certificate":                   {cert: root.CertPEM, key: root.KeyPEM, want: "is a CA certificate"},
		"key that does not match":          {cert: wildcard.CertPEM, key: other.KeyPEM, want: "does not match"},
		"certificate with no wildcard SAN": {cert: bare.CertPEM, key: bare.KeyPEM, want: "no wildcard"},
		"file without a certificate":       {cert: wildcard.KeyPEM, key: wildcard.KeyPEM, want: "no PEM-encoded certificate"},
		"key file without a key":           {cert: wildcard.CertPEM, key: wildcard.CertPEM, want: "no PEM-encoded private key"},
		"certificate file that is not PEM": {cert: []byte("not PEM"), key: wildcard.KeyPEM, want: "no PEM-encoded certificate"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withSecretsCache(t)
			o := &LocalSecretsAddTlsOptions{}
			if tc.cert != nil {
				o.CertFile = writeFiles(t, tc.cert)[0]
			}
			if tc.key != nil {
				o.KeyFile = writeFiles(t, tc.key)[0]
			}
			err := o.Run("workshop-tls")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run error = %v, want one mentioning %q", err, tc.want)
			}
			if names := cachedSecretNames(t); len(names) != 0 {
				t.Errorf("a rejected certificate left %v in the cache", names)
			}
		})
	}
}

func TestLocalSecretsAddTls_RefusesNamesTheCACacheManages(t *testing.T) {
	leaf := testpki.NewRootCA().NewLeaf(testpki.WithDNSNames("*.workshop.test"))

	for name, want := range map[string]string{
		"educates-ca-root-key-pair": "reserved",
		"workshop.test-ca-key-pair": "reserved",
		"educates-ca":               "root CA",
	} {
		t.Run(name, func(t *testing.T) {
			withSecretsCache(t)
			if err := (&LocalSecretsAddCaOptions{}).Run(&bytes.Buffer{}, "educates-ca"); err != nil {
				t.Fatalf("root: %v", err)
			}
			files := writeFiles(t, leaf.CertPEM, leaf.KeyPEM)

			err := (&LocalSecretsAddTlsOptions{CertFile: files[0], KeyFile: files[1]}).Run(name)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Run error = %v, want one mentioning %q", err, want)
			}
			if got, want := cachedSecretNames(t), []string{"educates-ca", "educates-ca-root-key-pair"}; !reflect.DeepEqual(got, want) {
				t.Errorf("cache holds %v, want %v", got, want)
			}
			assertRootRegistered(t, "educates-ca")
		})
	}
}
