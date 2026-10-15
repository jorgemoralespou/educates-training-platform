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
// Educates components.
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
)

// CACertificateKey is the key a CA trust Secret holds its PEM
// certificates under, and the only key a derived CA trust Secret holds.
const CACertificateKey = "ca.crt"

// Keys of a CA key pair Secret, the shape cert-manager's CA issuer reads.
const (
	tlsCertificateKey = "tls.crt"
	tlsPrivateKeyKey  = "tls.key"
)

// fieldManager and the managed-by label match what the rest of the
// operator applies, so a derived Secret shows the operator as its single
// owner.
const (
	fieldManager        = "educates-installer"
	managedByLabel      = "app.kubernetes.io/managed-by"
	managedByLabelValue = "educates-installer"
)

// CASourceRule says how a source Secret yields the CA certificate. Its
// value prefixes the reason of every SourceError the rule reports.
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
	ReasonCAKeyPairNotFound           = string(FromCAKeyPair) + "NotFound"
	ReasonCAKeyPairIncomplete         = string(FromCAKeyPair) + "Incomplete"
	ReasonCAKeyPairInvalidCertificate = string(FromCAKeyPair) + "InvalidCertificate"
	ReasonCAKeyPairNotCA              = string(FromCAKeyPair) + "NotCA"

	ReasonCATrustSecretNotFound           = string(FromCATrustSecret) + "NotFound"
	ReasonCATrustSecretIncomplete         = string(FromCATrustSecret) + "Incomplete"
	ReasonCATrustSecretInvalidCertificate = string(FromCATrustSecret) + "InvalidCertificate"
)

// CASource names the Secret a CA certificate is read from and the rule
// to read it by. Namespace is never defaulted here; callers resolve an
// empty reference namespace to the operator namespace first.
type CASource struct {
	Namespace string
	Name      string
	Rule      CASourceRule
}

// SourceError reports a source Secret that cannot yield a CA
// certificate: it is missing, lacks a required key, holds no parseable
// certificate, or, for a CA key pair, holds a certificate that is not a
// CA. Reason is a PascalCase condition reason; Message names the Secret.
type SourceError struct {
	Reason  string
	Message string
}

func (e *SourceError) Error() string {
	return e.Message
}

// CACertificate derives the PEM certificates for ca.crt from a source
// Secret by rule. The result holds only CERTIFICATE blocks, re-encoded,
// so it never carries a private key whatever else the source holds. A
// source that cannot yield a CA certificate returns a *SourceError.
func CACertificate(secret *corev1.Secret, rule CASourceRule) ([]byte, error) {
	id := secret.Namespace + "/" + secret.Name
	switch rule {
	case FromCATrustSecret:
		data := secret.Data[CACertificateKey]
		if len(data) == 0 {
			return nil, sourceError(rule, "Incomplete", "Secret %s is missing required key %q", id, CACertificateKey)
		}
		certs, err := parseCertificates(data)
		if err != nil {
			return nil, sourceError(rule, "InvalidCertificate", "Secret %s key %q: %v", id, CACertificateKey, err)
		}
		return encodeCertificates(certs), nil

	case FromCAKeyPair:
		for _, k := range []string{tlsCertificateKey, tlsPrivateKeyKey} {
			if len(secret.Data[k]) == 0 {
				return nil, sourceError(rule, "Incomplete", "Secret %s is missing required key %q", id, k)
			}
		}
		chain, err := parseCertificates(secret.Data[tlsCertificateKey])
		if err != nil {
			return nil, sourceError(rule, "InvalidCertificate", "Secret %s key %q: %v", id, tlsCertificateKey, err)
		}
		if !chain[0].IsCA {
			return nil, sourceError(rule, "NotCA", "Secret %s: the certificate in %q (subject %q) is not a CA certificate",
				id, tlsCertificateKey, chain[0].Subject.String())
		}
		if data := secret.Data[CACertificateKey]; len(data) > 0 {
			certs, err := parseCertificates(data)
			if err != nil {
				return nil, sourceError(rule, "InvalidCertificate", "Secret %s key %q: %v", id, CACertificateKey, err)
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
// *SourceError; any other read failure is returned as it is, for retry.
func ReadCACertificate(ctx context.Context, reader client.Reader, src CASource) ([]byte, error) {
	secret := &corev1.Secret{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: src.Namespace, Name: src.Name}, secret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, sourceError(src.Rule, "NotFound", "Secret %s/%s not found", src.Namespace, src.Name)
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
// yield a CA certificate returns a *SourceError and leaves dst as it
// was.
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
		WithLabels(map[string]string{managedByLabel: managedByLabelValue}).
		WithOwnerReferences(metav1ac.OwnerReference().
			WithAPIVersion(gvk.GroupVersion().String()).
			WithKind(gvk.Kind).
			WithName(owner.GetName()).
			WithUID(owner.GetUID()).
			WithController(true).
			WithBlockOwnerDeletion(true)).
		WithType(corev1.SecretTypeOpaque).
		WithData(map[string][]byte{CACertificateKey: caPEM})
	if err := a.Writer.Apply(ctx, secret, client.FieldOwner(fieldManager), client.ForceOwnership); err != nil {
		return fmt.Errorf("apply CA trust Secret %s: %w", dst, err)
	}
	return nil
}

func sourceError(rule CASourceRule, reason, format string, args ...any) *SourceError {
	return &SourceError{Reason: string(rule) + reason, Message: fmt.Sprintf(format, args...)}
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
	var out []byte
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
