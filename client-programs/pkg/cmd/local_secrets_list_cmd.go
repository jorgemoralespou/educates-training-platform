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
		if row := cachedSecretListRow(name, now); row != nil {
			rows = append(rows, row)
		}
	}

	_, err = fmt.Fprintln(w, utils.PrintTable([]string{"NAME", "TYPE", "DOMAINS", "EXPIRES"}, rows))
	return err
}

// cachedSecretListRow returns the list row of the cached Secret name, or
// nil when the cache no longer holds it.
func cachedSecretListRow(name string, now time.Time) []string {
	s, role, _, err := secrets.LoadClassifiedCachedSecret(name)
	if err != nil {
		return nil
	}

	row := []string{name, string(role), "", ""}
	if s == nil {
		// A file that cannot be read or decoded is valid for no domain.
		return row
	}
	row[2] = domainsCell(name, role, s)
	if cert := secrets.CachedSecretCertificate(role, s); cert != nil {
		row[3] = expiresCell(cert, now)
	}
	return row
}

// domainsCell is what the Secret s, cached under name with role role, is
// valid for, as the DOMAINS column and the Domains field of
// `educates local secrets view` show it: its domains comma separated, or
// "any" for a CA without permitted DNS name constraints.
func domainsCell(name string, role secrets.CachedSecretRole, s *apiv1.Secret) string {
	domains, anyDomain := secrets.CachedSecretDomains(name, role, s)
	if anyDomain {
		return "any"
	}
	return strings.Join(domains, ", ")
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
