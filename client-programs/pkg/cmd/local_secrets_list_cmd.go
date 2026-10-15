package cmd

import (
	"crypto/x509"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	apiv1 "k8s.io/api/core/v1"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localSecretsListExample = `
  # List the secrets in the local cache with their role, the domains
  # they are valid for, their certificate subject and expiry:
  educates local secrets list
`

type LocalSecretsListOptions struct{}

// Run prints the local secrets cache to w as a table with the columns
// NAME, TYPE, DOMAINS, SUBJECT and EXPIRES. TYPE is the role the local
// certificate resolution step gives the Secret, through
// secrets.ClassifyCachedSecret, so the table shows each Secret as a
// deploy treats it. A file that cannot be read or decoded is listed as
// invalid too, and the rest of the table still prints. Failing to read
// the cache directory is an error.
func (o *LocalSecretsListOptions) Run(w io.Writer) error {
	names, err := secrets.ListCachedSecretNames()
	if err != nil {
		return errors.Wrap(err, "unable to list cached secrets")
	}

	now := time.Now()
	rows := make([][]string, 0, len(names))
	for _, name := range names {
		rows = append(rows, cachedSecretListRow(name, now))
	}

	_, err = fmt.Fprintln(w, utils.PrintTable([]string{"NAME", "TYPE", "DOMAINS", "SUBJECT", "EXPIRES"}, rows))
	return err
}

// cachedSecretListRow returns the list row of the cached Secret name.
func cachedSecretListRow(name string, now time.Time) []string {
	s, err := secrets.LoadCachedSecret(name)
	if err != nil {
		return []string{name, string(secrets.InvalidRole), "", "", ""}
	}

	role := secrets.ClassifyCachedSecret(name, s)
	var cert *x509.Certificate
	var domains, subject string
	switch role {
	case secrets.CACertificateRole:
		cert = secrets.CATrustCertificates(s)[0]
		domains = permittedDomainsCell(cert)
	case secrets.CARootKeyPairRole:
		cert = firstTLSCertificate(s)
		if cert != nil {
			domains = permittedDomainsCell(cert)
		}
	case secrets.CAKeyPairRole:
		// A deploy uses the CA key pair <domain>-ca-key-pair for <domain>.
		cert = firstTLSCertificate(s)
		domains = strings.TrimSuffix(name, secrets.CAKeyPairSuffix)
		if root := s.Annotations[secrets.RootCAAnnotation]; root != "" {
			subject = fmt.Sprintf("%s (%s)", domains, root)
		}
	case secrets.TLSCertificateRole:
		cert = firstTLSCertificate(s)
		domains = strings.Join(secrets.WildcardDomains(cert), ", ")
	case secrets.InvalidRole:
		// No deploy uses it, so it is valid for no domain, but the
		// subject and expiry of the CA it holds identify it.
		cert = firstTLSCertificate(s)
	}

	row := []string{name, string(role), domains, subject, ""}
	if cert != nil {
		if subject == "" {
			row[3] = subjectCell(cert)
		}
		row[4] = expiresCell(cert, now)
	}
	return row
}

// firstTLSCertificate returns the first certificate of the tls.crt of s, or
// nil when it has none that parses.
func firstTLSCertificate(s *apiv1.Secret) *x509.Certificate {
	cert, err := secrets.ParseCertificatePEM(secrets.SecretData(s, "tls.crt"))
	if err != nil {
		return nil
	}
	return cert
}

// permittedDomainsCell is the DOMAINS cell of a CA: its permitted DNS name
// constraints, or "any" when it has none.
func permittedDomainsCell(ca *x509.Certificate) string {
	if len(ca.PermittedDNSDomains) == 0 {
		return "any"
	}
	return strings.Join(ca.PermittedDNSDomains, ", ")
}

// subjectCell is the SUBJECT cell of a certificate: its subject's common
// name, or the whole subject when it has none.
func subjectCell(cert *x509.Certificate) string {
	if cert.Subject.CommonName != "" {
		return cert.Subject.CommonName
	}
	return cert.Subject.String()
}

// expiresCell is the EXPIRES cell of a certificate: the date it expires, or
// "expired".
func expiresCell(cert *x509.Certificate, now time.Time) string {
	if now.After(cert.NotAfter) {
		return "expired"
	}
	return cert.NotAfter.UTC().Format(time.DateOnly)
}

func (p *ProjectInfo) NewLocalSecretsListCmd() *cobra.Command {
	var o LocalSecretsListOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "list",
		Short: "List secrets in the cache",
		Long: `List the secrets in the local secrets cache, with what each one is and
what a deploy uses it for.

TYPE is the role of the secret: CA certificate, CA root key pair, CA
intermediate key pair, TLS certificate, Registry credentials or Generic,
or invalid for a secret to remove with "educates local secrets remove
NAME". A file that cannot be read or decoded is invalid, and so is a CA
certificate with its private key that is neither a CA root key pair nor
a CA intermediate key pair, which is how 4.0.0 alpha releases cached a
CA: deploys neither use it nor copy it to a cluster. To keep using such
a CA, register it as the root CA with "educates local secrets add ca"
before removing it.

DOMAINS is what the secret is valid for: every wildcard domain of a TLS
certificate, the ingress domain of a CA intermediate key pair, and the
permitted DNS name constraints of a CA certificate or CA root key pair,
or "any" when it has none.

SUBJECT is the common name of the certificate's subject, or the whole
subject when it has none, and "<domain> (<root>)" for a CA intermediate
key pair. EXPIRES is the date the certificate expires, in UTC, or
"expired".`,
		Example: localSecretsListExample,
		RunE:    func(cmd *cobra.Command, _ []string) error { return o.Run(cmd.OutOrStdout()) },
	}

	return c
}
