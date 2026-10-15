package secrets

import (
	"crypto/x509"
	"fmt"

	apiv1 "k8s.io/api/core/v1"
)

// CachedSecretRole is what a Secret in the local secrets cache is for,
// named as `educates local secrets list` shows it. The local certificate
// resolution step reads the cache through the same classification, so
// the list always shows a Secret as a deploy treats it.
type CachedSecretRole string

const (
	// CACertificateRole is a Secret whose ca.crt holds only CA
	// certificates and which holds no CA private key, such as the CA
	// trust Secret of a root CA. Components trust the CA it holds.
	CACertificateRole CachedSecretRole = "CA certificate"

	// CARootKeyPairRole is the CA root key pair of a root CA, a Secret
	// named with RootKeyPairSuffix. Deploys mint CA key pairs from it,
	// and it never leaves this machine.
	CARootKeyPairRole CachedSecretRole = "CA root key pair"

	// CAKeyPairRole is the CA key pair of an ingress domain, an
	// intermediate CA minted from the root CA, named with CAKeyPairSuffix.
	// A deploy for that domain issues the ingress certificates from it.
	CAKeyPairRole CachedSecretRole = "CA intermediate key pair"

	// TLSCertificateRole is a kubernetes.io/tls Secret whose tls.crt
	// starts with a certificate that is not a CA. It serves every domain
	// it has a *.<domain> DNS name for; see WildcardDomains.
	TLSCertificateRole CachedSecretRole = "TLS certificate"

	// RegistryCredentialsRole is a kubernetes.io/dockerconfigjson Secret
	// holding container registry credentials.
	RegistryCredentialsRole CachedSecretRole = "Registry credentials"

	// GenericRole is any other Secret.
	GenericRole CachedSecretRole = "Generic"

	// InvalidRole is a cached entry that no deploy can use, to remove
	// from the cache. ClassifyCachedSecret and LoadClassifiedCachedSecret
	// say why, as an *InvalidCachedSecretError.
	InvalidRole CachedSecretRole = "invalid"
)

// InvalidCachedSecretProblem is why a cached entry is invalid.
type InvalidCachedSecretProblem int

const (
	// CachedSecretUnreadable is a cached file that cannot be read or
	// decoded as a Secret. LoadCachedSecret and
	// LoadClassifiedCachedSecret report it.
	CachedSecretUnreadable InvalidCachedSecretProblem = iota

	// CachedSecretUnmanagedCAKey is a Secret holding a CA certificate as
	// tls.crt with its private key as tls.key under a name that is
	// neither a CA root key pair nor a CA key pair, so not a CA key the
	// local secrets cache manages. It is how 4.0.0 alpha releases cached
	// a CA. Deploys neither use it nor copy it to a cluster.
	// ClassifyCachedSecret reports it.
	CachedSecretUnmanagedCAKey
)

// InvalidCachedSecretError says why the cached entry Name is invalid.
type InvalidCachedSecretError struct {
	Name    string
	Problem InvalidCachedSecretProblem

	// Err is the read or decode error, for CachedSecretUnreadable.
	Err error
}

func (e *InvalidCachedSecretError) Error() string {
	switch e.Problem {
	case CachedSecretUnreadable:
		return e.Err.Error()
	case CachedSecretUnmanagedCAKey:
		return fmt.Sprintf("cached secret %q holds a CA certificate with its private key under a name that is neither a CA root key pair nor a CA key pair, as 4.0.0 alpha releases cached a CA, so no deploy uses it", e.Name)
	}
	return fmt.Sprintf("cached secret %q is invalid", e.Name)
}

func (e *InvalidCachedSecretError) Unwrap() error {
	return e.Err
}

// ClassifyCachedSecret returns the role of the Secret s, cached under
// name, and for an invalid Secret why it is invalid, which is nil for
// every other role. The names of CA root key pairs and CA key pairs
// decide their roles, as they decide which root CA a deploy mints from
// and which CA key pair it uses. Any other Secret gets the first role
// that fits it, in the order: invalid, TLS certificate, CA certificate,
// registry credentials, generic. A TLS certificate that also carries CA
// certificates as ca.crt is classified as a TLS certificate, and a
// deploy does not trust those CA certificates: only a CA certificate is
// CA trust.
func ClassifyCachedSecret(name string, s *apiv1.Secret) (CachedSecretRole, *InvalidCachedSecretError) {
	if role, ok := ClassifyCachedSecretName(name); ok {
		return role, nil
	}
	if cert, err := ParseCertificatePEM(SecretData(s, "tls.crt")); err == nil {
		switch {
		case IsCACertificate(cert) && len(SecretData(s, "tls.key")) > 0:
			return InvalidRole, &InvalidCachedSecretError{Name: name, Problem: CachedSecretUnmanagedCAKey}
		case s.Type == apiv1.SecretTypeTLS && !IsCACertificate(cert):
			return TLSCertificateRole, nil
		}
	}
	if CATrustCertificates(s) != nil {
		return CACertificateRole, nil
	}
	if s.Type == apiv1.SecretTypeDockerConfigJson {
		return RegistryCredentialsRole, nil
	}
	return GenericRole, nil
}

// ClassifyCachedSecretName returns the role the name of a cached Secret
// decides on its own, a CA root key pair or a CA key pair, and false for
// any other name, whose role depends on what the Secret holds. It is
// the classification of a name that has no Secret yet, or whose Secret
// cannot be read.
func ClassifyCachedSecretName(name string) (CachedSecretRole, bool) {
	if _, ok := RootCAName(name); ok {
		return CARootKeyPairRole, true
	}
	if _, ok := CAKeyPairDomain(name); ok {
		return CAKeyPairRole, true
	}
	return "", false
}

// HoldsCAKey reports whether a Secret of role r holds a CA's private
// key. Such a Secret is copied to a cluster only when it is the CA key
// pair a deploy selected. A Secret ClassifyCachedSecret classifies as
// invalid holds one: it holds a CA key the local secrets cache does not
// manage.
func (r CachedSecretRole) HoldsCAKey() bool {
	return r == CARootKeyPairRole || r == CAKeyPairRole || r == InvalidRole
}

// HoldsCertificate reports whether a Secret of role r holds a
// certificate it is for: a CA certificate, a TLS certificate, or any
// Secret that holds a CA's private key, with the CA's certificate. An
// entry whose file cannot be read or decoded has the invalid role but no
// Secret, so no certificate.
func (r CachedSecretRole) HoldsCertificate() bool {
	return r == CACertificateRole || r == TLSCertificateRole || r.HoldsCAKey()
}

// CachedSecretCertificate returns the certificate the Secret s, of role
// role, is for: the first CA certificate of the ca.crt of a CA
// certificate, and the first certificate of the tls.crt of any other
// Secret that holds a certificate, which for an invalid Secret is a CA
// as 4.0.0 alpha releases cached one. It is nil for a Secret that holds
// no certificate, and when that certificate does not parse.
func CachedSecretCertificate(role CachedSecretRole, s *apiv1.Secret) *x509.Certificate {
	switch {
	case role == CACertificateRole:
		return CATrustCertificates(s)[0]
	case role.HoldsCertificate():
		cert, err := ParseCertificatePEM(SecretData(s, "tls.crt"))
		if err != nil {
			return nil
		}
		return cert
	}
	return nil
}

// CachedSecretDomains returns the ingress domains the Secret s, cached
// under name with role role, is valid for: every wildcard domain of a TLS
// certificate, the ingress domain of a CA key pair, which a deploy for
// that domain uses, and the permitted DNS name constraints of a CA
// certificate or CA root key pair. anyDomain is true for a CA certificate
// or CA root key pair without permitted DNS name constraints, which is
// valid for any domain. Any other Secret, including an invalid one, which
// no deploy uses, is valid for no domain.
func CachedSecretDomains(name string, role CachedSecretRole, s *apiv1.Secret) (domains []string, anyDomain bool) {
	switch role {
	case CACertificateRole, CARootKeyPairRole:
		if ca := CachedSecretCertificate(role, s); ca != nil {
			return ca.PermittedDNSDomains, len(ca.PermittedDNSDomains) == 0
		}
	case CAKeyPairRole:
		if domain, ok := CAKeyPairDomain(name); ok {
			return []string{domain}, false
		}
	case TLSCertificateRole:
		return WildcardDomains(CachedSecretCertificate(role, s)), false
	}
	return nil, false
}

// CATrustCertificates returns the CA certificates the ca.crt of s
// holds, or nil when ca.crt is missing, does not parse, or holds a
// certificate that is not a CA.
func CATrustCertificates(s *apiv1.Secret) []*x509.Certificate {
	cas, err := ParseCertificatesPEM(SecretData(s, "ca.crt"))
	if err != nil {
		return nil
	}
	for _, ca := range cas {
		if !IsCACertificate(ca) {
			return nil
		}
	}
	return cas
}

// SecretData returns the value of key in s, from data or, failing that,
// stringData.
func SecretData(s *apiv1.Secret, key string) []byte {
	if v, ok := s.Data[key]; ok {
		return v
	}
	if v, ok := s.StringData[key]; ok {
		return []byte(v)
	}
	return nil
}
