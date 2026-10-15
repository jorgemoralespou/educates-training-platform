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

package pki

import (
	"context"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// wildcardCertificateReasonPrefix prefixes the reason of every
// SourceError the wildcard certificate check reports.
const wildcardCertificateReasonPrefix = "WildcardCertificate"

// Reasons a wildcard certificate Secret cannot be served. They are
// condition reasons, so a reconciler can publish them as they are.
const (
	ReasonWildcardCertificateNotFound       = wildcardCertificateReasonPrefix + "NotFound"
	ReasonWildcardCertificateIncomplete     = wildcardCertificateReasonPrefix + "Incomplete"
	ReasonWildcardCertificateUnparseable    = wildcardCertificateReasonPrefix + "Unparseable"
	ReasonWildcardCertificateKeyMismatch    = wildcardCertificateReasonPrefix + "KeyMismatch"
	ReasonWildcardCertificateDomainMismatch = wildcardCertificateReasonPrefix + "DomainMismatch"
	ReasonWildcardCertificateExpired        = wildcardCertificateReasonPrefix + "Expired"
	ReasonWildcardCertificateUntrusted      = wildcardCertificateReasonPrefix + "Untrusted"
)

// WildcardRequirements are what a wildcard certificate must satisfy to
// be served for an ingress domain.
type WildcardRequirements struct {
	// Domain is the ingress domain. The certificate's SANs must include
	// *.<Domain>.
	Domain string

	// CA, when set, holds the PEM certificates of the CA the
	// certificate's chain must verify against, as CACertificate returns
	// them.
	CA []byte

	// Now is the time the certificate must not have expired by.
	Now time.Time
}

// WildcardCertificate is a wildcard certificate Secret that passed the
// check.
type WildcardCertificate struct {
	// Certificate and PrivateKey are the Secret's tls.crt and tls.key,
	// as given.
	Certificate []byte
	PrivateKey  []byte

	// NotAfter is when the certificate expires. It stops passing the
	// check after this time.
	NotAfter time.Time
}

// CheckWildcardCertificate checks that a Secret holds a wildcard
// certificate Educates can serve for the domain: tls.crt and tls.key
// parse, the key matches the first certificate of tls.crt, its SANs
// include *.<domain>, it has not expired, and, when a CA is given, its
// chain, through any intermediates tls.crt carries, verifies against
// that CA. Each failure is a *SourceError with its own reason.
func CheckWildcardCertificate(secret *corev1.Secret, want WildcardRequirements) (*WildcardCertificate, error) {
	id := secret.Namespace + "/" + secret.Name
	for _, k := range []string{tlsCertificateKey, tlsPrivateKeyKey} {
		if len(secret.Data[k]) == 0 {
			return nil, wildcardError("Incomplete", "Secret %s is missing required key %q", id, k)
		}
	}
	chain, err := parseCertificates(secret.Data[tlsCertificateKey])
	if err != nil {
		return nil, wildcardError("Unparseable", "Secret %s key %q: %v", id, tlsCertificateKey, err)
	}
	leaf := chain[0]
	key, err := parsePrivateKey(secret.Data[tlsPrivateKeyKey])
	if err != nil {
		return nil, wildcardError("Unparseable", "Secret %s key %q: %v", id, tlsPrivateKeyKey, err)
	}
	if !publicKeysEqual(leaf.PublicKey, key.Public()) {
		return nil, wildcardError("KeyMismatch", "Secret %s: the private key in %q does not match the certificate in %q",
			id, tlsPrivateKeyKey, tlsCertificateKey)
	}
	wildcard := "*." + strings.TrimSuffix(want.Domain, ".")
	if !hasDNSName(leaf, wildcard) {
		return nil, wildcardError("DomainMismatch", "Secret %s: the certificate's SANs %v do not include %q",
			id, leaf.DNSNames, wildcard)
	}
	if want.Now.After(leaf.NotAfter) {
		return nil, wildcardError("Expired", "Secret %s: the certificate expired at %s",
			id, leaf.NotAfter.UTC().Format(time.RFC3339))
	}
	if len(want.CA) > 0 {
		if err := verifyChain(chain, want.CA, want.Now); err != nil {
			return nil, wildcardError("Untrusted", "Secret %s: the certificate does not verify against the CA: %v", id, err)
		}
	}
	return &WildcardCertificate{
		Certificate: secret.Data[tlsCertificateKey],
		PrivateKey:  secret.Data[tlsPrivateKeyKey],
		NotAfter:    leaf.NotAfter,
	}, nil
}

// ReadWildcardCertificate reads a wildcard certificate Secret through
// reader and checks it with CheckWildcardCertificate. A missing Secret
// is a *SourceError; any other read failure is returned as it is, for
// retry.
func ReadWildcardCertificate(ctx context.Context, reader client.Reader, key types.NamespacedName, want WildcardRequirements) (*WildcardCertificate, error) {
	secret := &corev1.Secret{}
	if err := reader.Get(ctx, key, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, wildcardError("NotFound", "Secret %s not found", key)
		}
		return nil, fmt.Errorf("read Secret %s: %w", key, err)
	}
	return CheckWildcardCertificate(secret, want)
}

func wildcardError(reason, format string, args ...any) *SourceError {
	return &SourceError{Reason: wildcardCertificateReasonPrefix + reason, Message: fmt.Sprintf(format, args...)}
}

// parsePrivateKey parses the first private key PEM block of data, in
// any of the encodings TLS key pairs come in: PKCS #8, PKCS #1 RSA and
// SEC 1 EC.
func parsePrivateKey(data []byte) (crypto.Signer, error) {
	for rest := data; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, fmt.Errorf("no PEM private key found")
		}
		if !strings.HasSuffix(block.Type, "PRIVATE KEY") {
			continue
		}
		if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
			if signer, ok := key.(crypto.Signer); ok {
				return signer, nil
			}
			return nil, fmt.Errorf("unsupported private key type %T", key)
		}
		if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
			return key, nil
		}
		if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
			return key, nil
		}
		return nil, fmt.Errorf("parse private key: unsupported encoding in %q PEM block", block.Type)
	}
}

func publicKeysEqual(a, b crypto.PublicKey) bool {
	eq, ok := a.(interface{ Equal(crypto.PublicKey) bool })
	return ok && eq.Equal(b)
}

// hasDNSName reports whether the certificate's DNS SANs include name,
// compared without regard to case.
func hasDNSName(cert *x509.Certificate, name string) bool {
	for _, san := range cert.DNSNames {
		if strings.EqualFold(strings.TrimSuffix(san, "."), name) {
			return true
		}
	}
	return false
}

// verifyChain verifies the first certificate of chain against the CA
// certificates in caPEM, through the other certificates of chain as
// intermediates. Name constraints along the chain apply.
func verifyChain(chain []*x509.Certificate, caPEM []byte, now time.Time) error {
	cas, err := parseCertificates(caPEM)
	if err != nil {
		return fmt.Errorf("CA certificate: %w", err)
	}
	roots := x509.NewCertPool()
	for _, ca := range cas {
		roots.AddCert(ca)
	}
	intermediates := x509.NewCertPool()
	for _, cert := range chain[1:] {
		intermediates.AddCert(cert)
	}
	_, err = chain[0].Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   now,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	return err
}
