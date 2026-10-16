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

package testpki_test

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/educates/educates-training-platform/installer/operator/internal/testpki"
)

// parseCertPEM decodes exactly one CERTIFICATE block, the way a consumer
// of a Secret's tls.crt or ca.crt would.
func parseCertPEM(t *testing.T, data []byte) *x509.Certificate {
	t.Helper()
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("expected a CERTIFICATE PEM block, got %q", data)
	}
	if len(rest) != 0 {
		t.Fatalf("expected a single PEM block, found trailing data %q", rest)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

func TestNewRootCA_IsSelfSignedCAWithMatchingKey(t *testing.T) {
	root := testpki.NewRootCA(testpki.WithCommonName("Example Root"))

	cert := parseCertPEM(t, root.CertPEM)
	if !cert.IsCA {
		t.Errorf("root certificate is not a CA")
	}
	if cert.Subject.CommonName != "Example Root" {
		t.Errorf("subject CN = %q, want %q", cert.Subject.CommonName, "Example Root")
	}
	if err := cert.CheckSignatureFrom(cert); err != nil {
		t.Errorf("root is not self-signed: %v", err)
	}
	if _, err := tls.X509KeyPair(root.CertPEM, root.KeyPEM); err != nil {
		t.Errorf("root key does not match its certificate: %v", err)
	}
}

func TestNewLeaf_HasRequestedSANsAndValidityAndVerifiesToItsRoot(t *testing.T) {
	root := testpki.NewRootCA()
	notBefore := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter := time.Date(2036, 1, 1, 0, 0, 0, 0, time.UTC)

	leaf := root.NewLeaf(
		testpki.WithDNSNames("*.example.test", "example.test"),
		testpki.WithIPAddresses(net.ParseIP("10.10.10.1")),
		testpki.WithValidity(notBefore, notAfter),
	)

	cert := parseCertPEM(t, leaf.CertPEM)
	if cert.IsCA {
		t.Errorf("leaf certificate is a CA")
	}
	if got, want := cert.DNSNames, []string{"*.example.test", "example.test"}; !slices.Equal(got, want) {
		t.Errorf("DNS SANs = %v, want %v", got, want)
	}
	if len(cert.IPAddresses) != 1 || !cert.IPAddresses[0].Equal(net.ParseIP("10.10.10.1")) {
		t.Errorf("IP SANs = %v, want [10.10.10.1]", cert.IPAddresses)
	}
	if !cert.NotBefore.Equal(notBefore) || !cert.NotAfter.Equal(notAfter) {
		t.Errorf("validity = %v..%v, want %v..%v", cert.NotBefore, cert.NotAfter, notBefore, notAfter)
	}
	if _, err := tls.X509KeyPair(leaf.CertPEM, leaf.KeyPEM); err != nil {
		t.Errorf("leaf key does not match its certificate: %v", err)
	}

	_, err := cert.Verify(x509.VerifyOptions{
		Roots:       poolOf(root),
		DNSName:     "workshop.example.test",
		CurrentTime: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Errorf("leaf does not verify against its root: %v", err)
	}
}

func TestNewLeaf_ExpiredLeafFailsVerification(t *testing.T) {
	root := testpki.NewRootCA()
	leaf := root.NewLeaf(
		testpki.WithDNSNames("*.example.test"),
		testpki.WithValidity(time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour)),
	)

	_, err := parseCertPEM(t, leaf.CertPEM).Verify(x509.VerifyOptions{Roots: poolOf(root)})
	var invalid x509.CertificateInvalidError
	if !errors.As(err, &invalid) || invalid.Reason != x509.Expired {
		t.Errorf("verify error = %v, want an x509.Expired CertificateInvalidError", err)
	}
}

func TestNewLeaf_FromAnotherRootFailsVerification(t *testing.T) {
	root := testpki.NewRootCA()
	other := testpki.NewRootCA()
	leaf := other.NewLeaf(testpki.WithDNSNames("*.example.test"))

	_, err := parseCertPEM(t, leaf.CertPEM).Verify(x509.VerifyOptions{Roots: poolOf(root)})
	var unknown x509.UnknownAuthorityError
	if !errors.As(err, &unknown) {
		t.Errorf("verify error = %v, want an x509.UnknownAuthorityError", err)
	}
}

func TestNewIntermediateCA_ChainVerifiesLeafToRoot(t *testing.T) {
	root := testpki.NewRootCA()
	intermediate := root.NewIntermediateCA(testpki.WithCommonName("Example Intermediate"))
	leaf := intermediate.NewLeaf(testpki.WithDNSNames("*.example.test"))

	interCert := parseCertPEM(t, intermediate.CertPEM)
	if !interCert.IsCA {
		t.Errorf("intermediate certificate is not a CA")
	}
	if interCert.Subject.CommonName != "Example Intermediate" {
		t.Errorf("subject CN = %q, want %q", interCert.Subject.CommonName, "Example Intermediate")
	}
	if _, err := tls.X509KeyPair(intermediate.CertPEM, intermediate.KeyPEM); err != nil {
		t.Errorf("intermediate key does not match its certificate: %v", err)
	}

	// A CA key pair's tls.crt holds the intermediate followed by the root.
	if got := chainCommonNames(t, intermediate.ChainPEM()); !slices.Equal(got, []string{"Example Intermediate", "testpki root CA"}) {
		t.Errorf("intermediate chain = %v, want [Example Intermediate, testpki root CA]", got)
	}

	// A wildcard Secret's tls.crt carries the leaf and its issuers; the
	// pair loads and the leaf verifies to the root through the chain.
	pair, err := tls.X509KeyPair(leaf.ChainPEM(), leaf.KeyPEM)
	if err != nil {
		t.Fatalf("leaf chain and key do not form a key pair: %v", err)
	}
	intermediates := x509.NewCertPool()
	for _, der := range pair.Certificate[1:] {
		c, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatalf("parse chain certificate: %v", err)
		}
		intermediates.AddCert(c)
	}
	_, err = parseCertPEM(t, leaf.CertPEM).Verify(x509.VerifyOptions{
		Roots:         poolOf(root),
		Intermediates: intermediates,
		DNSName:       "workshop.example.test",
	})
	if err != nil {
		t.Errorf("leaf does not verify to the root through its chain: %v", err)
	}
}

func TestNewIntermediateCA_NameConstraintsLimitWhatItCanSign(t *testing.T) {
	root := testpki.NewRootCA()
	intermediate := root.NewIntermediateCA(
		testpki.WithPermittedDNSDomains("example.test"),
		testpki.WithExcludedIPRanges("0.0.0.0/0", "::/0"),
		testpki.WithMaxPathLen(0),
	)

	interCert := parseCertPEM(t, intermediate.CertPEM)
	if !slices.Equal(interCert.PermittedDNSDomains, []string{"example.test"}) {
		t.Errorf("permitted DNS domains = %v, want [example.test]", interCert.PermittedDNSDomains)
	}
	if !interCert.PermittedDNSDomainsCritical {
		t.Errorf("name constraints extension is not critical")
	}
	if len(interCert.ExcludedIPRanges) != 2 {
		t.Errorf("excluded IP ranges = %v, want 0.0.0.0/0 and ::/0", interCert.ExcludedIPRanges)
	}
	if interCert.MaxPathLen != 0 || !interCert.MaxPathLenZero {
		t.Errorf("path length = %d (zero=%t), want 0", interCert.MaxPathLen, interCert.MaxPathLenZero)
	}

	verify := func(leaf *testpki.Certificate) error {
		intermediates := x509.NewCertPool()
		for ca := leaf.Issuer; ca != root; ca = ca.Issuer {
			intermediates.AddCert(ca.Cert)
		}
		_, err := parseCertPEM(t, leaf.CertPEM).Verify(x509.VerifyOptions{
			Roots:         poolOf(root),
			Intermediates: intermediates,
		})
		return err
	}
	if err := verify(intermediate.NewLeaf(testpki.WithDNSNames("*.example.test"))); err != nil {
		t.Errorf("leaf inside the permitted domain does not verify: %v", err)
	}
	for name, tc := range map[string]struct {
		leaf   *testpki.Certificate
		reason x509.InvalidReason
	}{
		"DNS name outside the permitted domain": {
			intermediate.NewLeaf(testpki.WithDNSNames("*.other.test")), x509.CANotAuthorizedForThisName},
		"excluded IP address": {
			intermediate.NewLeaf(testpki.WithIPAddresses(net.ParseIP("10.10.10.1"))), x509.CANotAuthorizedForThisName},
		"sub-CA beyond the path length": {
			intermediate.NewIntermediateCA().NewLeaf(testpki.WithDNSNames("*.example.test")), x509.TooManyIntermediates},
	} {
		err := verify(tc.leaf)
		var invalid x509.CertificateInvalidError
		if !errors.As(err, &invalid) || invalid.Reason != tc.reason {
			t.Errorf("%s: verify error = %v, want CertificateInvalidError reason %d", name, err, tc.reason)
		}
	}
}

// chainCommonNames returns the subject CN of each CERTIFICATE block in
// data, in order.
func chainCommonNames(t *testing.T, data []byte) []string {
	t.Helper()
	var names []string
	for block, rest := pem.Decode(data); block != nil; block, rest = pem.Decode(rest) {
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("parse chain certificate: %v", err)
		}
		names = append(names, cert.Subject.CommonName)
	}
	return names
}

// poolOf returns a CertPool trusting the given CAs' certificates.
func poolOf(cas ...*testpki.Certificate) *x509.CertPool {
	pool := x509.NewCertPool()
	for _, ca := range cas {
		pool.AppendCertsFromPEM(ca.CertPEM)
	}
	return pool
}
