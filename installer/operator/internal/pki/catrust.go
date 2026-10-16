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

// Package pki holds the operator's certificate handling that more than
// one reconciler shares. Its CA trust derivation turns a Secret supplied
// by the user, which may hold a private key, into a key-free CA trust
// Secret holding only ca.crt: the only form in which CA trust reaches
// Educates components. Its wildcard certificate check decides whether a
// wildcard certificate Secret supplied by the user can be served for
// the ingress domain, with one reason per problem.
package pki

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"github.com/educates/educates-training-platform/installer/operator/internal/ownership"
)

// CACertificateKey is the key a CA trust Secret holds its PEM
// certificates under, and the only key a derived CA trust Secret holds.
const CACertificateKey = "ca.crt"

// CASourceRule says how a source Secret yields the CA certificate. Its
// value prefixes the reason of every CertificateSecretError the rule
// reports.
type CASourceRule string

const (
	// FromCATrustSecret reads the certificates under ca.crt. Any other
	// key, a private key included, is ignored.
	FromCATrustSecret CASourceRule = "CATrustSecret"

	// FromCAKeyPair reads a CA key pair, which must hold tls.crt and
	// tls.key, with a CA certificate first in tls.crt. The CA trust is
	// its ca.crt when present, else the root-most certificate of the
	// tls.crt chain, the certificate cert-manager's CA issuer puts in the
	// ca.crt of what it issues.
	FromCAKeyPair CASourceRule = "CAKeyPair"
)

// Reasons a source Secret cannot yield a CA certificate. They are
// condition reasons, so a reconciler can publish them as they are.
const (
	ReasonCAKeyPairNotFound    = string(FromCAKeyPair) + "NotFound"
	ReasonCAKeyPairIncomplete  = string(FromCAKeyPair) + "Incomplete"
	ReasonCAKeyPairUnparseable = string(FromCAKeyPair) + "Unparseable"
	ReasonCAKeyPairNotCA       = string(FromCAKeyPair) + "NotCA"

	ReasonCATrustSecretNotFound    = string(FromCATrustSecret) + "NotFound"
	ReasonCATrustSecretIncomplete  = string(FromCATrustSecret) + "Incomplete"
	ReasonCATrustSecretUnparseable = string(FromCATrustSecret) + "Unparseable"
)

// CASource names the Secret a CA certificate is read from and the rule
// to read it by. Namespace is never defaulted here; callers resolve an
// empty reference namespace to the operator namespace first.
type CASource struct {
	Namespace string
	Name      string
	Rule      CASourceRule
}

// CertificateSecretError reports a Secret holding certificates that the
// operator cannot use: a CA source that cannot yield a CA certificate,
// or a wildcard certificate Secret that cannot be served. Reason is a
// PascalCase condition reason; Message names the Secret.
type CertificateSecretError struct {
	Reason  string
	Message string
}

func (e *CertificateSecretError) Error() string {
	return e.Message
}

// CACertificate derives the PEM certificates for ca.crt from a source
// Secret by rule. The result holds only CERTIFICATE blocks, re-encoded,
// so it never carries a private key whatever else the source holds. A
// source that cannot yield a CA certificate returns a
// *CertificateSecretError.
func CACertificate(secret *corev1.Secret, rule CASourceRule) ([]byte, error) {
	id := secret.Namespace + "/" + secret.Name
	switch rule {
	case FromCATrustSecret:
		data := secret.Data[CACertificateKey]
		if len(data) == 0 {
			return nil, certificateSecretError(ReasonCATrustSecretIncomplete,
				"Secret %s is missing required key %q", id, CACertificateKey)
		}
		certs, err := parseCertificates(data)
		if err != nil {
			return nil, certificateSecretError(ReasonCATrustSecretUnparseable,
				"Secret %s key %q: %v", id, CACertificateKey, err)
		}
		return encodeCertificates(certs), nil

	case FromCAKeyPair:
		for _, k := range []string{corev1.TLSCertKey, corev1.TLSPrivateKeyKey} {
			if len(secret.Data[k]) == 0 {
				return nil, certificateSecretError(ReasonCAKeyPairIncomplete,
					"Secret %s is missing required key %q", id, k)
			}
		}
		chain, err := parseCertificates(secret.Data[corev1.TLSCertKey])
		if err != nil {
			return nil, certificateSecretError(ReasonCAKeyPairUnparseable,
				"Secret %s key %q: %v", id, corev1.TLSCertKey, err)
		}
		if !chain[0].IsCA {
			return nil, certificateSecretError(ReasonCAKeyPairNotCA,
				"Secret %s: the certificate in %q (subject %q) is not a CA certificate",
				id, corev1.TLSCertKey, chain[0].Subject.String())
		}
		if data := secret.Data[CACertificateKey]; len(data) > 0 {
			certs, err := parseCertificates(data)
			if err != nil {
				return nil, certificateSecretError(ReasonCAKeyPairUnparseable,
					"Secret %s key %q: %v", id, CACertificateKey, err)
			}
			return encodeCertificates(certs), nil
		}
		return encodeCertificates([]*x509.Certificate{rootMost(chain)}), nil

	default:
		return nil, fmt.Errorf("unknown CA source rule %q", rule)
	}
}

// ReadCACertificate reads the source Secret through reader and derives
// its CA certificate by the source's rule. A missing Secret is a
// *CertificateSecretError; any other read failure is returned as it is,
// for retry.
func ReadCACertificate(ctx context.Context, reader client.Reader, src CASource) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: src.Namespace, Name: src.Name}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, certificateSecretError(src.Rule.notFoundReason(),
				"Secret %s/%s not found", src.Namespace, src.Name)
		}
		return nil, fmt.Errorf("read Secret %s/%s: %w", src.Namespace, src.Name, err)
	}
	return CACertificate(secret, src.Rule)
}

// CATrustApplier derives key-free CA trust Secrets and writes them.
type CATrustApplier struct {
	// Reader reads source Secrets. Use an uncached reader: a source can
	// live in a namespace outside the operator's Secret cache.
	Reader client.Reader

	// Writer server-side applies the derived Secret.
	Writer client.Writer

	// Scheme resolves the owner's group, version and kind.
	Scheme *runtime.Scheme
}

// Apply derives the CA certificate from src and server-side applies dst
// as an Opaque Secret holding only ca.crt, labelled as managed by the
// operator and controlled by owner, so it follows its source on every
// call and is garbage collected with its owner. A source that cannot
// yield a CA certificate returns a *CertificateSecretError and leaves
// dst as it was.
func (a CATrustApplier) Apply(ctx context.Context, src CASource, dst types.NamespacedName, owner client.Object) error {
	caPEM, err := ReadCACertificate(ctx, a.Reader, src)
	if err != nil {
		return err
	}
	gvk, err := apiutil.GVKForObject(owner, a.Scheme)
	if err != nil {
		return fmt.Errorf("resolve owner GVK: %w", err)
	}
	secret := corev1ac.Secret(dst.Name, dst.Namespace).
		WithLabels(map[string]string{ownership.ManagedByLabel: ownership.ManagedByValue}).
		WithOwnerReferences(metav1ac.OwnerReference().
			WithAPIVersion(gvk.GroupVersion().String()).
			WithKind(gvk.Kind).
			WithName(owner.GetName()).
			WithUID(owner.GetUID()).
			WithController(true).
			WithBlockOwnerDeletion(true)).
		WithType(corev1.SecretTypeOpaque).
		WithData(map[string][]byte{CACertificateKey: caPEM})
	if err := a.Writer.Apply(ctx, secret, client.FieldOwner(ownership.FieldManager), client.ForceOwnership); err != nil {
		return fmt.Errorf("apply CA trust Secret %s: %w", dst, err)
	}
	return nil
}

// notFoundReason is the reason a missing source Secret reports under
// the rule.
func (rule CASourceRule) notFoundReason() string {
	if rule == FromCAKeyPair {
		return ReasonCAKeyPairNotFound
	}
	return ReasonCATrustSecretNotFound
}

func certificateSecretError(reason, format string, args ...any) *CertificateSecretError {
	return &CertificateSecretError{Reason: reason, Message: fmt.Sprintf(format, args...)}
}

// parseCertificates parses every CERTIFICATE block of PEM data, in
// order, skipping blocks of any other type. It fails when a certificate
// does not parse or when there is none.
func parseCertificates(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	for rest := data; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse certificate: %w", err)
		}
		certs = append(certs, cert)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("no PEM certificate found")
	}
	return certs, nil
}

func encodeCertificates(certs []*x509.Certificate) []byte {
	out := make([]byte, 0, len(certs)*1024)
	for _, cert := range certs {
		out = append(out, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})...)
	}
	return out
}

// rootMost walks up from the first certificate of chain through issuers
// found in chain and returns the last one reached: the self-signed root
// when the chain carries it, else the highest certificate it carries.
func rootMost(chain []*x509.Certificate) *x509.Certificate {
	cur := chain[0]
	// Each step moves to a different certificate, so len(chain) steps
	// bound the walk even for a chain that loops.
	for range chain {
		if isSelfSigned(cur) {
			return cur
		}
		next := issuerIn(chain, cur)
		if next == nil {
			return cur
		}
		cur = next
	}
	return cur
}

func issuerIn(chain []*x509.Certificate, cert *x509.Certificate) *x509.Certificate {
	for _, candidate := range chain {
		if bytes.Equal(candidate.Raw, cert.Raw) {
			continue
		}
		if bytes.Equal(cert.RawIssuer, candidate.RawSubject) && cert.CheckSignatureFrom(candidate) == nil {
			return candidate
		}
	}
	return nil
}

func isSelfSigned(cert *x509.Certificate) bool {
	return bytes.Equal(cert.RawIssuer, cert.RawSubject) && cert.CheckSignatureFrom(cert) == nil
}
