// Package localcerts is the CLI's local certificate resolution step. From
// the local secrets cache and an EducatesLocalConfig's ingress.domain and
// ingress.insecure, it decides how a local install gets its ingress
// certificates and returns that decision as a Plan. Every command that
// renders, deploys or syncs a local install takes its certificate
// decisions from here and nowhere else.
//
// A secure install issues its ingress certificate from the CA key pair
// <domain>-ca-key-pair: an intermediate CA limited to the ingress domain,
// minted from the one root CA in the cache. The root's key never leaves
// this machine, and the cluster trusts the root through its CA trust
// Secret.
package localcerts

import (
	"crypto"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	apiv1 "k8s.io/api/core/v1"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// Plan is the outcome of resolving the certificates of a local install.
type Plan struct {
	// Certificates is the certificate decision the translator emits as
	// EducatesClusterConfig ingress.certificates.
	Certificates translator.LocalCertificates

	// SyncSecretNames names the cached Secrets a deploy pushes into the
	// secrets.SyncNamespace namespace, sorted. It includes a CA key pair
	// the plan mints, which is in the cache once the plan is committed.
	SyncSecretNames []string

	// Notices tell the user about the decision.
	Notices []string

	// Warnings tell the user about a problem the decision works around.
	Warnings []string

	// mints are the CA key pairs the plan minted, which Commit writes to
	// the cache.
	mints []mint
}

// mint is a CA key pair minted for the plan and the line Commit prints
// when it writes it.
type mint struct {
	secret *apiv1.Secret
	note   string
}

// Reporter receives the lines a plan reports. The deploy progress
// reporter satisfies it.
type Reporter interface {
	Note(msg string)
}

// Resolve decides the certificates of a local install from cfg's
// ingress.domain and ingress.insecure and the local secrets cache. It
// only reads the cache: a CA key pair it mints is held in the plan until
// Commit. Host-IP defaulting of ingress.domain must already have run.
func Resolve(cfg *v1alpha1.EducatesLocalConfig) (*Plan, error) {
	names, err := secrets.ListCachedSecretNames()
	if err != nil {
		return nil, err
	}
	plan := &Plan{}
	if cfg.Ingress.Insecure {
		// Plain HTTP needs no CA.
		plan.Certificates = translator.LocalCertificates{Provider: translator.CertificatesProviderNone}
		plan.SyncSecretNames = syncSecretNames(names, "")
		return plan, nil
	}

	domain := cfg.Ingress.Domain
	if domain == "" {
		return nil, fmt.Errorf("ingress.domain is empty; cannot decide the ingress certificates")
	}

	// A root CA mints the CA key pair cert-manager issues the ingress
	// certificate from.
	root, err := loadRootCA(domain, names)
	if err != nil {
		return nil, err
	}
	keyPair, err := plan.caKeyPairFor(domain, root)
	if err != nil {
		return nil, err
	}
	plan.Certificates = translator.LocalCertificates{
		Provider: translator.CertificatesProviderBundledCertManager,
		CustomCA: &translator.LocalCustomCA{
			CAKeyPairSecretRef:     translator.SecretRef{Name: keyPair, Namespace: secrets.SyncNamespace},
			CACertificateSecretRef: &translator.SecretRef{Name: root.name, Namespace: secrets.SyncNamespace},
		},
	}
	plan.SyncSecretNames = syncSecretNames(names, keyPair)
	return plan, nil
}

// ResolveSync resolves what `educates local secrets sync` pushes into the
// secrets.SyncNamespace namespace. With a local config at
// <data-home>/config.yaml that has an ingress.domain, it is the plan
// Resolve makes for that config. Without one, or for a config whose
// install derives its domain from the host IP and so serves plain HTTP,
// no CA key pair is selected and none is pushed. It only reads the cache
// and the config.
func ResolveSync() (*Plan, error) {
	cfg, err := loadLocalConfig()
	if err != nil {
		return nil, err
	}
	if cfg != nil && cfg.Ingress.Domain != "" {
		return Resolve(cfg)
	}
	names, err := secrets.ListCachedSecretNames()
	if err != nil {
		return nil, err
	}
	return &Plan{SyncSecretNames: syncSecretNames(names, "")}, nil
}

// loadLocalConfig loads <data-home>/config.yaml. It returns nil when the
// file does not exist or holds another kind than EducatesLocalConfig.
func loadLocalConfig() (*v1alpha1.EducatesLocalConfig, error) {
	path := filepath.Join(utils.GetEducatesHomeDir(), "config.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	local, _ := cfg.(*v1alpha1.EducatesLocalConfig)
	return local, nil
}

// rootCA is the root CA of the local secrets cache, read from its CA root
// key pair.
type rootCA struct {
	name    string
	cert    *x509.Certificate
	certPEM []byte
	key     crypto.Signer
}

// loadRootCA loads the one root CA of the cache, checking it can mint a
// CA for domain.
func loadRootCA(domain string, names []string) (*rootCA, error) {
	var roots []string
	for _, name := range names {
		if root, ok := strings.CutSuffix(name, secrets.RootKeyPairSuffix); ok {
			roots = append(roots, root)
		}
	}
	switch len(roots) {
	case 0:
		return nil, noRootCAError(domain)
	case 1:
	default:
		return nil, fmt.Errorf(`the local secrets cache holds more than one root CA: %s.

It can hold only one root CA, from which deploys mint a CA for each
ingress domain. Remove the root CAs you do not use with:

  educates local secrets remove NAME`, quoteAll(roots))
	}

	name := roots[0]
	keyPairName := secrets.RootKeyPairName(name)
	unusable := func(format string, args ...any) error {
		return fmt.Errorf("root CA %q cannot be used: %s", name, fmt.Sprintf(format, args...))
	}
	keyPair, err := secrets.LoadCachedSecret(keyPairName)
	if err != nil {
		return nil, unusable("%v", err)
	}
	cert, err := secrets.ParseCertificatePEM(keyPair.Data["tls.crt"])
	if err != nil {
		return nil, unusable("tls.crt of its CA root key pair %q: %v", keyPairName, err)
	}
	if !secrets.IsCACertificate(cert) {
		return nil, unusable("the certificate of its CA root key pair %q is not a CA certificate", keyPairName)
	}
	key, err := secrets.ParsePrivateKeyPEM(keyPair.Data["tls.key"])
	if err != nil {
		return nil, unusable("tls.key of its CA root key pair %q: %v", keyPairName, err)
	}
	if !secrets.KeyMatchesCertificate(cert, key) {
		return nil, unusable("the private key of its CA root key pair %q does not match its certificate", keyPairName)
	}
	if now := time.Now(); now.After(cert.NotAfter) {
		return nil, unusable("it expired on %s; register a new root CA", cert.NotAfter.UTC().Format(time.DateOnly))
	}

	// The cluster trusts the root through its CA trust Secret, so the
	// Secret must hold the root's own certificate.
	trust, err := secrets.LoadCachedSecret(name)
	if err != nil {
		return nil, unusable("its CA trust Secret %q is missing from the local secrets cache", name)
	}
	trustCert, err := secrets.ParseCertificatePEM(trust.Data["ca.crt"])
	if err != nil || !trustCert.Equal(cert) {
		return nil, unusable("its CA trust Secret %q does not hold the root certificate as ca.crt", name)
	}

	wildcard := "*." + domain
	if !secrets.PermitsDNSName(cert, wildcard) {
		return nil, fmt.Errorf(`root CA %q cannot mint a CA for ingress domain %q: its name
constraints do not permit %q (permitted DNS domains: %s; excluded DNS
domains: %s).

Set ingress.domain to a domain the root CA permits, or register a root CA
whose name constraints permit this domain.`,
			name, domain, wildcard, listOrNone(cert.PermittedDNSDomains), listOrNone(cert.ExcludedDNSDomains))
	}

	return &rootCA{name: name, cert: cert, certPEM: secrets.EncodeCertificatePEM(cert), key: key}, nil
}

// noRootCAError is the error for a secure install whose cache has no
// root CA, naming the commands that fix it.
func noRootCAError(domain string) error {
	return fmt.Errorf(`no root CA in the local secrets cache to issue certificates for ingress
domain %q.

A secure install is the default (ingress.insecure is false): Educates
serves workshops over HTTPS with certificates from a CA it mints for the
ingress domain from a root CA cached on this machine. The root's private
key never leaves this machine, and you trust its certificate once for
every local ingress domain.

To generate a root CA:

  educates local secrets add ca <name>

To register an existing root CA with its private key, such as mkcert's:

  educates local secrets add ca <name> --cert "$(mkcert -CAROOT)/rootCA.pem" --key "$(mkcert -CAROOT)/rootCA-key.pem"

To serve plain HTTP with no CA instead:

  educates local config set ingress.insecure true

The CA minted for the domain is pushed into the %q namespace at deploy
time; the root's private key is not.`, domain, secrets.SyncNamespace)
}

// caKeyPairFor returns the name of the CA key pair for domain. When the
// cache has no CA key pair for domain that root minted and that is still
// valid, it mints one into the plan for Commit to write.
func (p *Plan) caKeyPairFor(domain string, root *rootCA) (string, error) {
	name := secrets.CAKeyPairName(domain)
	note := fmt.Sprintf("Minted CA key pair %q for ingress domain %q from root CA %q.", name, domain, root.name)
	if cached, err := secrets.LoadCachedSecret(name); err == nil {
		stale := caKeyPairStaleness(cached, domain, root)
		if stale == "" {
			return name, nil
		}
		note = fmt.Sprintf("Minted CA key pair %q for ingress domain %q from root CA %q, replacing the cached one, which %s.", name, domain, root.name, stale)
	}

	certPEM, keyPEM, err := secrets.MintDomainCA(root.cert, root.key, domain)
	if err != nil {
		return "", fmt.Errorf("mint CA key pair %q from root CA %q: %w", name, root.name, err)
	}
	p.mints = append(p.mints, mint{
		secret: secrets.NewCAKeyPairSecret(domain, root.name, certPEM, keyPEM, root.certPEM),
		note:   note,
	})
	return name, nil
}

// caKeyPairStaleness returns why the cached CA key pair s cannot serve
// domain under root, or "" when it can.
func caKeyPairStaleness(s *apiv1.Secret, domain string, root *rootCA) string {
	const invalid = "is not a valid CA key pair"
	if s.Type != apiv1.SecretTypeTLS {
		return invalid
	}
	chain, err := secrets.ParseCertificatesPEM(s.Data["tls.crt"])
	if err != nil {
		return invalid
	}
	ca := chain[0]
	key, err := secrets.ParsePrivateKeyPEM(s.Data["tls.key"])
	if err != nil || !secrets.IsCACertificate(ca) || !secrets.KeyMatchesCertificate(ca, key) {
		return invalid
	}
	if !reflect.DeepEqual(ca.PermittedDNSDomains, []string{domain}) {
		return "is not limited to the domain"
	}
	caCrt, err := secrets.ParseCertificatePEM(s.Data["ca.crt"])
	if s.Annotations[secrets.RootCAAnnotation] != root.name ||
		err != nil || !caCrt.Equal(root.cert) ||
		ca.CheckSignatureFrom(root.cert) != nil {
		return "was minted from another root CA"
	}
	if time.Now().After(ca.NotAfter) {
		return "has expired"
	}
	return ""
}

// syncSecretNames returns the cached Secrets a sync pushes, sorted: every
// cached Secret except those holding a CA's private key, plus selected,
// the CA key pair the plan selected, when not empty.
func syncSecretNames(names []string, selected string) []string {
	var out []string
	for _, name := range names {
		if name != selected && !holdsCAKey(name) {
			out = append(out, name)
		}
	}
	if selected != "" {
		out = append(out, selected)
		sort.Strings(out)
	}
	return out
}

// holdsCAKey reports whether the cached Secret name holds a CA's private
// key: a CA root key pair, a CA key pair, or any Secret whose tls.key
// belongs to a CA certificate, such as a CA cached by an alpha release. A
// Secret that cannot be read does not count; the sync reports it.
func holdsCAKey(name string) bool {
	if strings.HasSuffix(name, secrets.RootKeyPairSuffix) || strings.HasSuffix(name, secrets.CAKeyPairSuffix) {
		return true
	}
	s, err := secrets.LoadCachedSecret(name)
	if err != nil || len(s.Data["tls.key"]) == 0 {
		return false
	}
	cert, err := secrets.ParseCertificatePEM(s.Data["tls.crt"])
	return err == nil && secrets.IsCACertificate(cert)
}

func quoteAll(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, ", ")
}

func listOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

// Report prints the plan's notices and then its warnings to r, without
// changing anything.
func (p *Plan) Report(r Reporter) {
	for _, n := range p.Notices {
		r.Note(n)
	}
	for _, w := range p.Warnings {
		r.Note("warning: " + w)
	}
}

// Commit carries out what the plan changes in the local cache, writing
// each CA key pair it minted and printing a line for each to r, and then
// reports the plan to r. Deploy and sync paths call it once before
// pushing to a cluster; render only calls Report, so rendering leaves the
// cache untouched.
func (p *Plan) Commit(r Reporter) error {
	for _, m := range p.mints {
		if err := secrets.WriteCachedSecret(m.secret); err != nil {
			return err
		}
		r.Note(m.note)
	}
	p.mints = nil
	p.Report(r)
	return nil
}

// TranslatorOptions returns the translator input the plan decided.
func (p *Plan) TranslatorOptions() translator.Options {
	return translator.Options{LocalCertificates: p.Certificates}
}
