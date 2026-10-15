// Package localcerts is the CLI's local certificate resolution step. From
// the local secrets cache and an EducatesLocalConfig's ingress.domain and
// ingress.insecure, it decides how a local install gets its ingress
// certificates and returns that decision as a Plan. Every command that
// renders, deploys or syncs a local install takes its certificate
// decisions from here and nowhere else.
package localcerts

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

// Plan is the outcome of resolving the certificates of a local install.
type Plan struct {
	// Certificates is the certificate decision the translator emits as
	// EducatesClusterConfig ingress.certificates.
	Certificates translator.LocalCertificates

	// SyncSecretNames names the cached Secrets a deploy pushes into the
	// secrets.SyncNamespace namespace, sorted.
	SyncSecretNames []string

	// Notices tell the user about the decision.
	Notices []string

	// Warnings tell the user about a problem the decision works around.
	Warnings []string
}

// Reporter receives the lines a plan reports. The deploy progress
// reporter satisfies it.
type Reporter interface {
	Note(msg string)
}

// Resolve decides the certificates of a local install from cfg's
// ingress.domain and ingress.insecure and the local secrets cache. It
// only reads the cache. Host-IP defaulting of ingress.domain must already
// have run.
func Resolve(cfg *v1alpha1.EducatesLocalConfig) (*Plan, error) {
	names, err := secrets.ListCachedSecretNames()
	if err != nil {
		return nil, err
	}
	plan := &Plan{SyncSecretNames: names}
	if cfg.Ingress.Insecure {
		// Plain HTTP needs no CA.
		plan.Certificates = translator.LocalCertificates{Provider: translator.CertificatesProviderNone}
		return plan, nil
	}

	caName, err := cachedCAForDomain(cfg.Ingress.Domain)
	if err != nil {
		return nil, err
	}
	plan.Certificates = translator.LocalCertificates{
		Provider: translator.CertificatesProviderBundledCertManager,
		CustomCA: &translator.LocalCustomCA{
			CAKeyPairSecretRef: translator.SecretRef{Name: caName, Namespace: secrets.SyncNamespace},
		},
	}
	return plan, nil
}

// ResolveSync returns the names of the cached Secrets
// `educates local secrets sync` pushes into the secrets.SyncNamespace
// namespace, sorted. It needs no local config and only reads the cache.
func ResolveSync() ([]string, error) {
	return secrets.ListCachedSecretNames()
}

// cachedCAForDomain finds the cached CA Secret whose
// 'training.educates.dev/domain' annotation matches domain. It errors
// with the commands that fix the cache when nothing matches.
func cachedCAForDomain(domain string) (string, error) {
	if domain == "" {
		return "", fmt.Errorf("ingress.domain is empty; cannot look up cached CA")
	}
	name := secrets.LocalCachedSecretForCertificateAuthority(domain)
	if name == "" {
		return "", fmt.Errorf(`no cached CA Secret found for domain %q.

A secure install is the default (ingress.insecure is false): Educates
serves workshops over HTTPS from a CA you supply, so it needs a cached CA
Secret whose 'training.educates.dev/domain' annotation matches this
domain. Either provide a CA for the domain, or set ingress.insecure to
true to serve plain HTTP with no CA.

To generate and cache a self-signed CA for the domain:

  educates local secrets add ca <name> --domain %s

To use an existing CA instead, pass its certificate and key:

  educates local secrets add ca <name> --domain %s --cert ca.crt --key ca.key

To serve plain HTTP with no CA instead:

  educates local config set ingress.insecure true

The cached CA Secret is pushed into the %q namespace at deploy time.`,
			domain, domain, domain, secrets.SyncNamespace)
	}
	return name, nil
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

// Commit carries out what the plan changes in the local cache and reports
// the plan to r. Deploy paths call it once before installing; render only
// calls Report, so rendering leaves the cache untouched.
func (p *Plan) Commit(r Reporter) error {
	p.Report(r)
	return nil
}

// TranslatorOptions returns the translator input the plan decided.
func (p *Plan) TranslatorOptions() translator.Options {
	return translator.Options{LocalCertificates: p.Certificates}
}
