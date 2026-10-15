/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pki_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/educates/educates-training-platform/installer/operator/internal/pki"
	"github.com/educates/educates-training-platform/installer/operator/internal/testpki"
)

func TestCheckWildcardCertificate_AcceptsAUsableCertificate(t *testing.T) {
	root := testpki.NewRootCA()
	inter := root.NewIntermediateCA(testpki.WithPermittedDNSDomains("example.test"))
	leaf := inter.NewLeaf(testpki.WithDNSNames("example.test", "*.example.test"))
	otherDomains := root.NewLeaf(testpki.WithDNSNames("*.other.test", "*.EXAMPLE.test"))

	cases := []struct {
		name string
		data map[string][]byte
		want pki.WildcardRequirements
		cert *testpki.Certificate
	}{
		{
			name: "chain verifying against the CA root",
			data: map[string][]byte{"tls.crt": leaf.ChainPEM(), "tls.key": leaf.KeyPEM},
			want: pki.WildcardRequirements{Domain: "example.test", CA: root.CertPEM},
			cert: leaf,
		},
		{
			name: "leaf with its intermediate verifying against the root, root not in tls.crt",
			data: map[string][]byte{"tls.crt": concat(leaf.CertPEM, inter.CertPEM), "tls.key": leaf.KeyPEM},
			want: pki.WildcardRequirements{Domain: "example.test", CA: root.CertPEM},
			cert: leaf,
		},
		{
			name: "no CA to verify against",
			data: map[string][]byte{"tls.crt": leaf.CertPEM, "tls.key": leaf.KeyPEM},
			want: pki.WildcardRequirements{Domain: "example.test"},
			cert: leaf,
		},
		{
			name: "one of several wildcard SANs, matched without regard to case",
			data: map[string][]byte{"tls.crt": otherDomains.CertPEM, "tls.key": otherDomains.KeyPEM},
			want: pki.WildcardRequirements{Domain: "example.test", CA: root.CertPEM},
			cert: otherDomains,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.want.Now = time.Now()
			got, err := pki.CheckWildcardCertificate(secretWith(tc.data), tc.want)
			if err != nil {
				t.Fatalf("CheckWildcardCertificate: %v", err)
			}
			if !bytes.Equal(got.Certificate, tc.data["tls.crt"]) || !bytes.Equal(got.PrivateKey, tc.data["tls.key"]) {
				t.Errorf("CheckWildcardCertificate did not return tls.crt and tls.key as given")
			}
			if !got.NotAfter.Equal(tc.cert.Cert.NotAfter) {
				t.Errorf("NotAfter = %v, want the leaf's %v", got.NotAfter, tc.cert.Cert.NotAfter)
			}
		})
	}
}

func TestCheckWildcardCertificate_AcceptsEveryPrivateKeyEncoding(t *testing.T) {
	root := testpki.NewRootCA()

	ecLeaf := root.NewLeaf(testpki.WithDNSNames("*.example.test"))
	sec1, err := x509.MarshalECPrivateKey(ecLeaf.Key.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}

	// mkcert and openssl issue RSA keys, as PKCS #8 and PKCS #1.
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	rsaDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "rsa leaf"},
		DNSNames:     []string{"*.example.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}, root.Cert, rsaKey.Public(), root.Key)
	if err != nil {
		t.Fatal(err)
	}
	rsaCertPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rsaDER})
	pkcs8, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		certPEM []byte
		keyPEM  []byte
	}{
		{"PKCS #8 EC", ecLeaf.CertPEM, ecLeaf.KeyPEM},
		{"SEC 1 EC", ecLeaf.CertPEM, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: sec1})},
		{"PKCS #8 RSA", rsaCertPEM, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})},
		{"PKCS #1 RSA", rsaCertPEM, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(rsaKey)})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pki.CheckWildcardCertificate(secretWith(map[string][]byte{
				"tls.crt": tc.certPEM,
				"tls.key": tc.keyPEM,
			}), pki.WildcardRequirements{Domain: "example.test", CA: root.CertPEM, Now: time.Now()})
			if err != nil {
				t.Fatalf("CheckWildcardCertificate: %v", err)
			}
		})
	}
}

func TestCheckWildcardCertificate_RejectsUnusableCertificates(t *testing.T) {
	root := testpki.NewRootCA()
	leaf := root.NewLeaf(testpki.WithDNSNames("*.example.test"))
	other := root.NewLeaf(testpki.WithDNSNames("*.example.test"))
	foreign := testpki.NewRootCA().NewLeaf(testpki.WithDNSNames("*.example.test"))
	expired := root.NewLeaf(testpki.WithDNSNames("*.example.test"),
		testpki.WithValidity(time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour)))
	hostsOnly := root.NewLeaf(testpki.WithDNSNames("example.test", "www.example.test"))
	subdomain := root.NewLeaf(testpki.WithDNSNames("*.sub.example.test"))
	constrained := root.NewIntermediateCA(testpki.WithPermittedDNSDomains("other.test"))
	outsideConstraint := constrained.NewLeaf(testpki.WithDNSNames("*.example.test"))

	cases := []struct {
		name       string
		data       map[string][]byte
		ca         []byte
		wantReason string
	}{
		{
			name:       "without tls.crt",
			data:       map[string][]byte{"tls.key": leaf.KeyPEM},
			wantReason: pki.ReasonWildcardCertificateIncomplete,
		},
		{
			name:       "without tls.key",
			data:       map[string][]byte{"tls.crt": leaf.CertPEM},
			wantReason: pki.ReasonWildcardCertificateIncomplete,
		},
		{
			name:       "tls.crt is not a certificate",
			data:       map[string][]byte{"tls.crt": []byte("not a certificate"), "tls.key": leaf.KeyPEM},
			wantReason: pki.ReasonWildcardCertificateUnparseable,
		},
		{
			name:       "tls.key is not a private key",
			data:       map[string][]byte{"tls.crt": leaf.CertPEM, "tls.key": leaf.CertPEM},
			wantReason: pki.ReasonWildcardCertificateUnparseable,
		},
		{
			name:       "tls.key belongs to another certificate",
			data:       map[string][]byte{"tls.crt": leaf.CertPEM, "tls.key": other.KeyPEM},
			wantReason: pki.ReasonWildcardCertificateKeyMismatch,
		},
		{
			name:       "SANs name hosts but no wildcard",
			data:       map[string][]byte{"tls.crt": hostsOnly.CertPEM, "tls.key": hostsOnly.KeyPEM},
			wantReason: pki.ReasonWildcardCertificateDomainMismatch,
		},
		{
			name:       "wildcard of a subdomain only",
			data:       map[string][]byte{"tls.crt": subdomain.CertPEM, "tls.key": subdomain.KeyPEM},
			wantReason: pki.ReasonWildcardCertificateDomainMismatch,
		},
		{
			name:       "expired",
			data:       map[string][]byte{"tls.crt": expired.CertPEM, "tls.key": expired.KeyPEM},
			ca:         root.CertPEM,
			wantReason: pki.ReasonWildcardCertificateExpired,
		},
		{
			name:       "issued by another CA",
			data:       map[string][]byte{"tls.crt": foreign.ChainPEM(), "tls.key": foreign.KeyPEM},
			ca:         root.CertPEM,
			wantReason: pki.ReasonWildcardCertificateUntrusted,
		},
		{
			name:       "issued by an intermediate whose name constraints exclude the domain",
			data:       map[string][]byte{"tls.crt": outsideConstraint.ChainPEM(), "tls.key": outsideConstraint.KeyPEM},
			ca:         root.CertPEM,
			wantReason: pki.ReasonWildcardCertificateUntrusted,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pki.CheckWildcardCertificate(secretWith(tc.data), pki.WildcardRequirements{
				Domain: "example.test",
				CA:     tc.ca,
				Now:    time.Now(),
			})
			var serr *pki.CertificateSecretError
			if !errors.As(err, &serr) {
				t.Fatalf("CheckWildcardCertificate error = %v, want a *pki.CertificateSecretError", err)
			}
			if serr.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q (message %q)", serr.Reason, tc.wantReason, serr.Message)
			}
		})
	}
}
