// Package testpki builds real x509 certificates for tests: self-signed
// root CAs, intermediate CAs and leaf certificates, each returned with
// its PEM-encoded certificate and private key so tests can place them in
// Secrets the way users and cert-manager do. Every certificate gets its
// own ECDSA P-256 key.
//
// It is a non-test package so every test suite in the CLI module can
// import it. Production code must not.
//
// The constructors panic on failure. Key generation and signing only fail
// on a broken template, which is a bug in the test, and panicking keeps
// call sites to one line.
package testpki

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// Certificate is a generated certificate with its private key.
type Certificate struct {
	// Cert is the parsed certificate.
	Cert *x509.Certificate
	// Key is the certificate's private key.
	Key crypto.Signer
	// CertPEM is the certificate as a single CERTIFICATE PEM block.
	CertPEM []byte
	// KeyPEM is the private key as a PKCS #8 PRIVATE KEY PEM block.
	KeyPEM []byte
	// Issuer is the CA that signed the certificate, or nil for a
	// self-signed root.
	Issuer *Certificate
}

// Option customizes the certificate template before it is signed. Tests
// can pass their own func to set a field no With option covers.
type Option func(*x509.Certificate)

// WithCommonName sets the subject common name.
func WithCommonName(cn string) Option {
	return func(tmpl *x509.Certificate) {
		tmpl.Subject = pkix.Name{CommonName: cn}
	}
}

// WithDNSNames sets the DNS subject alternative names.
func WithDNSNames(names ...string) Option {
	return func(tmpl *x509.Certificate) {
		tmpl.DNSNames = names
	}
}

// WithIPAddresses sets the IP address subject alternative names.
func WithIPAddresses(ips ...net.IP) Option {
	return func(tmpl *x509.Certificate) {
		tmpl.IPAddresses = ips
	}
}

// WithValidity sets the validity window. A window in the past gives an
// expired certificate; one in the future gives a not-yet-valid one.
func WithValidity(notBefore, notAfter time.Time) Option {
	return func(tmpl *x509.Certificate) {
		tmpl.NotBefore = notBefore
		tmpl.NotAfter = notAfter
	}
}

// WithPermittedDNSDomains adds a critical name constraint permitting
// only the given DNS domains and their subdomains. It is meaningful on a
// CA certificate.
func WithPermittedDNSDomains(domains ...string) Option {
	return func(tmpl *x509.Certificate) {
		tmpl.PermittedDNSDomains = domains
		tmpl.PermittedDNSDomainsCritical = true
	}
}

// WithExcludedDNSDomains adds a name constraint excluding the given DNS
// domains and their subdomains. It is meaningful on a CA certificate.
func WithExcludedDNSDomains(domains ...string) Option {
	return func(tmpl *x509.Certificate) {
		tmpl.ExcludedDNSDomains = domains
	}
}

// WithExcludedIPRanges adds a name constraint excluding the given CIDR
// ranges, for example "0.0.0.0/0" and "::/0" to exclude every IP
// address. It is meaningful on a CA certificate and panics on an
// invalid CIDR.
func WithExcludedIPRanges(cidrs ...string) Option {
	ranges := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(fmt.Sprintf("testpki: excluded IP range %q: %v", cidr, err))
		}
		ranges = append(ranges, ipNet)
	}
	return func(tmpl *x509.Certificate) {
		tmpl.ExcludedIPRanges = ranges
	}
}

// WithMaxPathLen limits how many CAs may follow this one in a chain; 0
// allows it to sign only leaf certificates. It is meaningful on a CA
// certificate.
func WithMaxPathLen(n int) Option {
	return func(tmpl *x509.Certificate) {
		tmpl.MaxPathLen = n
		tmpl.MaxPathLenZero = n == 0
	}
}

// NewRootCA returns a self-signed root CA. Without options it is named
// "testpki root CA" and is valid from an hour ago for a year.
func NewRootCA(opts ...Option) *Certificate {
	return sign(caTemplate("testpki root CA"), opts, nil)
}

// NewIntermediateCA returns a CA certificate signed by ca. Without
// options it is named "testpki intermediate CA" and is valid from an
// hour ago for a year.
func (ca *Certificate) NewIntermediateCA(opts ...Option) *Certificate {
	return sign(caTemplate("testpki intermediate CA"), opts, ca)
}

// NewLeaf returns a server certificate signed by ca. Without options it
// is named "testpki leaf", has no subject alternative names and is valid
// from an hour ago for a year.
func (ca *Certificate) NewLeaf(opts ...Option) *Certificate {
	tmpl := baseTemplate("testpki leaf")
	tmpl.KeyUsage = x509.KeyUsageDigitalSignature
	tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	return sign(tmpl, opts, ca)
}

// ChainPEM returns the certificate followed by each of its issuers, up
// to and including the root, as concatenated PEM blocks. This is the
// shape of a tls.crt that carries its chain.
func (c *Certificate) ChainPEM() []byte {
	var chain []byte
	for cert := c; cert != nil; cert = cert.Issuer {
		chain = append(chain, cert.CertPEM...)
	}
	return chain
}

func caTemplate(cn string) *x509.Certificate {
	tmpl := baseTemplate(cn)
	tmpl.IsCA = true
	tmpl.BasicConstraintsValid = true
	tmpl.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature
	return tmpl
}

func baseTemplate(cn string) *x509.Certificate {
	now := time.Now()
	return &x509.Certificate{
		SerialNumber: newSerial(),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(1, 0, 0),
	}
}

// sign applies opts to tmpl and signs it with a new key, by issuer, or
// by the new key itself when issuer is nil.
func sign(tmpl *x509.Certificate, opts []Option, issuer *Certificate) *Certificate {
	for _, opt := range opts {
		opt(tmpl)
	}
	key := newKey()
	parent, issuerKey := tmpl, key
	if issuer != nil {
		parent, issuerKey = issuer.Cert, issuer.Key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, key.Public(), issuerKey)
	if err != nil {
		panic(fmt.Sprintf("testpki: sign certificate %q: %v", tmpl.Subject.CommonName, err))
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		panic(fmt.Sprintf("testpki: parse certificate %q: %v", tmpl.Subject.CommonName, err))
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		panic(fmt.Sprintf("testpki: marshal key: %v", err))
	}
	return &Certificate{
		Cert:    cert,
		Key:     key,
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		Issuer:  issuer,
	}
}

func newKey() crypto.Signer {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("testpki: generate key: %v", err))
	}
	return key
}

func newSerial() *big.Int {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		panic(fmt.Sprintf("testpki: generate serial: %v", err))
	}
	return serial
}
