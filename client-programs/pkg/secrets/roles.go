package secrets

import (
	"crypto/x509"
	"strings"

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

	// UnusedCAKeyPairRole is any other Secret holding a CA certificate
	// as tls.crt with its private key as tls.key, such as a CA cached by
	// a 4.0.0 alpha release. Deploys neither use it nor copy it to a
	// cluster.
	UnusedCAKeyPairRole CachedSecretRole = "Unused CA key pair"

	// RegistryCredentialsRole is a kubernetes.io/dockerconfigjson Secret
	// holding container registry credentials.
	RegistryCredentialsRole CachedSecretRole = "Registry credentials"

	// GenericRole is any other Secret.
	GenericRole CachedSecretRole = "Generic"
)

// ClassifyCachedSecret returns the role of the Secret s, cached under
// name. The names of CA root key pairs and CA key pairs decide their
// roles, as they decide which root CA a deploy mints from and which CA
// key pair it uses. Any other Secret gets the first role that fits it,
// in the order: unused CA key pair, TLS certificate, CA certificate,
// registry credentials, generic. A TLS certificate that also carries CA
// certificates as ca.crt is classified as a TLS certificate, although
// those CA certificates can still verify another TLS certificate's
// chain.
func ClassifyCachedSecret(name string, s *apiv1.Secret) CachedSecretRole {
	if role, ok := ClassifyCachedSecretName(name); ok {
		return role
	}
	if cert, err := ParseCertificatePEM(SecretData(s, "tls.crt")); err == nil {
		switch {
		case IsCACertificate(cert) && len(SecretData(s, "tls.key")) > 0:
			return UnusedCAKeyPairRole
		case s.Type == apiv1.SecretTypeTLS && !IsCACertificate(cert):
			return TLSCertificateRole
		}
	}
	if CATrustCertificates(s) != nil {
		return CACertificateRole
	}
	if s.Type == apiv1.SecretTypeDockerConfigJson {
		return RegistryCredentialsRole
	}
	return GenericRole
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
	if strings.HasSuffix(name, CAKeyPairSuffix) {
		return CAKeyPairRole, true
	}
	return "", false
}

// HoldsCAKey reports whether a Secret of role r holds a CA's private
// key. Such a Secret is copied to a cluster only when it is the CA key
// pair a deploy selected.
func (r CachedSecretRole) HoldsCAKey() bool {
	return r == CARootKeyPairRole || r == CAKeyPairRole || r == UnusedCAKeyPairRole
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
