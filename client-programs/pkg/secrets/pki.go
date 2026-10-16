package secrets

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sort"
	"strings"
)

// ParseCertificatePEM returns the first certificate in the PEM data,
// skipping any other kind of PEM block before it.
func ParseCertificatePEM(data []byte) (*x509.Certificate, error) {
	certs, err := ParseCertificatesPEM(data)
	if err != nil {
		return nil, err
	}
	return certs[0], nil
}

// ParseCertificatesPEM returns every certificate in the PEM data, in
// order, skipping any other kind of PEM block. It errors when the data
// holds no certificate or a certificate that does not parse.
func ParseCertificatesPEM(data []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
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
		return nil, fmt.Errorf("no PEM-encoded certificate found")
	}
	return certs, nil
}

// ParsePrivateKeyPEM returns the first private key in the PEM data, in
// PKCS #8, PKCS #1 (RSA) or SEC 1 (EC) form.
func ParsePrivateKeyPEM(data []byte) (crypto.Signer, error) {
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("no PEM-encoded private key found")
		}
		if !strings.HasSuffix(block.Type, "PRIVATE KEY") {
			continue
		}
		var key any
		var err error
		switch block.Type {
		case "RSA PRIVATE KEY":
			key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		case "EC PRIVATE KEY":
			key, err = x509.ParseECPrivateKey(block.Bytes)
		default:
			key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		signer, ok := key.(crypto.Signer)
		if !ok {
			return nil, fmt.Errorf("unsupported private key type %T", key)
		}
		return signer, nil
	}
}

// KeyPair is a certificate, with any chain that follows it, and the
// private key of that certificate.
type KeyPair struct {
	// Chain is the certificate first, then any certificates after it.
	Chain []*x509.Certificate

	// Key is the private key of Chain[0].
	Key crypto.Signer
}

// Certificate is the key pair's certificate, the first of its chain.
func (kp *KeyPair) Certificate() *x509.Certificate {
	return kp.Chain[0]
}

// KeyPairProblem is why PEM data is not a usable key pair.
type KeyPairProblem int

const (
	// KeyPairCertificateUnparseable is certificate PEM data holding no
	// certificate, or one that does not parse.
	KeyPairCertificateUnparseable KeyPairProblem = iota

	// KeyPairKeyUnparseable is private key PEM data holding no private
	// key, or one that does not parse.
	KeyPairKeyUnparseable

	// KeyPairKeyMismatch is a private key that is not the certificate's.
	KeyPairKeyMismatch
)

// KeyPairError reports PEM data that is not a usable key pair. Callers
// that know where the data came from, a file or a Secret key, phrase
// their message from Problem.
type KeyPairError struct {
	Problem KeyPairProblem

	// Err is the parse error, for the two unparseable problems.
	Err error
}

func (e *KeyPairError) Error() string {
	switch e.Problem {
	case KeyPairCertificateUnparseable:
		return "certificate: " + e.Err.Error()
	case KeyPairKeyUnparseable:
		return "private key: " + e.Err.Error()
	default:
		return "the private key does not match the certificate"
	}
}

func (e *KeyPairError) Unwrap() error {
	return e.Err
}

// ParseKeyPair parses certPEM, a certificate optionally followed by its
// chain, and keyPEM, the private key of that certificate, and checks the
// key matches the certificate. Its error is a *KeyPairError.
func ParseKeyPair(certPEM, keyPEM []byte) (*KeyPair, error) {
	chain, err := ParseCertificatesPEM(certPEM)
	if err != nil {
		return nil, &KeyPairError{Problem: KeyPairCertificateUnparseable, Err: err}
	}
	key, err := ParsePrivateKeyPEM(keyPEM)
	if err != nil {
		return nil, &KeyPairError{Problem: KeyPairKeyUnparseable, Err: err}
	}
	if !KeyMatchesCertificate(chain[0], key) {
		return nil, &KeyPairError{Problem: KeyPairKeyMismatch}
	}
	return &KeyPair{Chain: chain, Key: key}, nil
}

// KeyMatchesCertificate reports whether key is the private key of cert.
func KeyMatchesCertificate(cert *x509.Certificate, key crypto.Signer) bool {
	pub, ok := cert.PublicKey.(interface{ Equal(crypto.PublicKey) bool })
	return ok && pub.Equal(key.Public())
}

// IsCACertificate reports whether cert may sign other certificates.
func IsCACertificate(cert *x509.Certificate) bool {
	return cert.BasicConstraintsValid && cert.IsCA
}

// WildcardDomains returns the domains cert is a wildcard certificate
// for: <domain> for each *.<domain> DNS SAN, lowercased, without a
// trailing dot, sorted and without duplicates.
func WildcardDomains(cert *x509.Certificate) []string {
	seen := map[string]bool{}
	var domains []string
	for _, san := range cert.DNSNames {
		domain, ok := strings.CutPrefix(strings.ToLower(strings.TrimSuffix(san, ".")), "*.")
		if !ok || domain == "" || seen[domain] {
			continue
		}
		seen[domain] = true
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}

// EncodeCertificatePEM returns cert as a CERTIFICATE PEM block.
func EncodeCertificatePEM(cert *x509.Certificate) []byte {
	return encodeCertificateDER(cert.Raw)
}

// EncodeCertificatesPEM returns certs as CERTIFICATE PEM blocks, in
// order.
func EncodeCertificatesPEM(certs []*x509.Certificate) []byte {
	var out []byte
	for _, cert := range certs {
		out = append(out, EncodeCertificatePEM(cert)...)
	}
	return out
}

// encodeCertificateDER returns the DER-encoded certificate der as a
// CERTIFICATE PEM block.
func encodeCertificateDER(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// EncodePrivateKeyPEM returns key as a PKCS #8 PRIVATE KEY PEM block.
func EncodePrivateKeyPEM(key crypto.Signer) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}
