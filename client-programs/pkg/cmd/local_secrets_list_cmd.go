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
  # they are valid for and their expiry:
  educates local secrets list
`

type LocalSecretsListOptions struct{}

// Run prints the local secrets cache to w as a table with the columns
// NAME, TYPE, DOMAINS and EXPIRES. TYPE is the role the local certificate
// resolution step gives the Secret, through secrets.ClassifyCachedSecret,
// so the table shows each Secret as a deploy treats it. A file that
// cannot be read or decoded is listed as invalid too, and the rest of the
// table still prints. Failing to read the cache directory is an error.
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

	_, err = fmt.Fprintln(w, utils.PrintTable([]string{"NAME", "TYPE", "DOMAINS", "EXPIRES"}, rows))
	return err
}

// cachedSecretListRow returns the list row of the cached Secret name.
func cachedSecretListRow(name string, now time.Time) []string {
	s, err := secrets.LoadCachedSecret(name)
	if err != nil {
		return []string{name, string(secrets.InvalidRole), "", ""}
	}

	role := secrets.ClassifyCachedSecret(name, s)
	row := []string{name, string(role), cachedSecretDomains(name, role, s), ""}
	if cert := cachedSecretCertificate(role, s); cert != nil {
		row[3] = expiresCell(cert, now)
	}
	return row
}

// cachedSecretDomains returns what the Secret s, cached under name with
// role role, is valid for, as the DOMAINS column shows it: every wildcard
// domain of a TLS certificate, the ingress domain of a CA key pair, and
// the permitted DNS name constraints of a CA certificate or CA root key
// pair, or "any" when it has none. It is empty for any other Secret,
// including an invalid one, which no deploy uses.
func cachedSecretDomains(name string, role secrets.CachedSecretRole, s *apiv1.Secret) string {
	switch role {
	case secrets.CACertificateRole, secrets.CARootKeyPairRole:
		if cert := cachedSecretCertificate(role, s); cert != nil {
			return permittedDomainsCell(cert)
		}
	case secrets.CAKeyPairRole:
		// A deploy uses the CA key pair <domain>-ca-key-pair for <domain>.
		return strings.TrimSuffix(name, secrets.CAKeyPairSuffix)
	case secrets.TLSCertificateRole:
		return strings.Join(secrets.WildcardDomains(cachedSecretCertificate(role, s)), ", ")
	}
	return ""
}

// cachedSecretCertificate returns the certificate that the Secret s, of
// role role, is for, whose expiry the EXPIRES column shows: the first CA
// certificate of the ca.crt of a CA certificate, and the first
// certificate of the tls.crt of a CA root key pair, a CA key pair, a TLS
// certificate or an invalid Secret, which may hold a CA as 4.0.0 alpha
// releases cached one. It is nil for any other Secret, and when that
// certificate does not parse.
func cachedSecretCertificate(role secrets.CachedSecretRole, s *apiv1.Secret) *x509.Certificate {
	switch role {
	case secrets.CACertificateRole:
		return secrets.CATrustCertificates(s)[0]
	case secrets.CARootKeyPairRole, secrets.CAKeyPairRole, secrets.TLSCertificateRole, secrets.InvalidRole:
		return firstTLSCertificate(s)
	}
	return nil
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

EXPIRES is the date the certificate expires, in UTC, or "expired".

"educates local secrets view NAME" shows the details of one secret, such
as the subject, issuer and name constraints of its certificates, or why
it is invalid.`,
		Example: localSecretsListExample,
		RunE:    func(cmd *cobra.Command, _ []string) error { return o.Run(cmd.OutOrStdout()) },
	}

	return c
}
