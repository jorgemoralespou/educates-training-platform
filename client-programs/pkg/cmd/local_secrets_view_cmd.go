package cmd

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	apiv1 "k8s.io/api/core/v1"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

var localSecretsViewExample = `
  # Show the details of a cached secret, such as the subject, issuer,
  # validity and name constraints of each certificate it holds:
  educates local secrets view educates-ca
`

type LocalSecretsViewOptions struct{}

// Run prints to w the details of the cached Secret name that do not fit
// the table of `educates local secrets list`, as labelled lines. Its role
// and domains are those the list shows, through the same classification,
// so the two always agree. It prints no key material: of a private key
// only whether the Secret holds one, of registry credentials only their
// registries, and of a generic Secret only the names of its keys. For an
// invalid entry it prints why it is invalid and the command that removes
// it, after, for a CA cached by a 4.0.0 alpha release, how to keep the CA.
// A name the cache does not hold is an error.
func (o *LocalSecretsViewOptions) Run(w io.Writer, name string) error {
	s, role, invalid, err := secrets.LoadClassifiedCachedSecret(name)
	if err != nil {
		return err
	}

	var v secretViewPrinter
	v.field("Name", name)
	v.field("Type", string(role))
	v.field("Location", secrets.CachedSecretPath(name))
	if s != nil {
		v.secret(name, role, s, time.Now())
	}
	if invalid != nil {
		v.invalid(name, invalid)
	}

	_, err = w.Write(v.buf.Bytes())
	return err
}

// secret prints the fields of the Secret s, cached under name with role
// role, that follow its location.
func (v *secretViewPrinter) secret(name string, role secrets.CachedSecretRole, s *apiv1.Secret, now time.Time) {
	v.field("Secret type", secretTypeOf(s))
	if domains := cachedSecretDomains(name, role, s); domains != "" {
		v.field("Domains", domains)
	}
	if role == secrets.CAKeyPairRole {
		// The ingress domain the CA key pair was minted for, and the root
		// CA it was minted from, as it records them.
		if domain := s.Annotations[secrets.DomainAnnotation]; domain != "" {
			v.field("Ingress domain", domain)
		}
		if root := s.Annotations[secrets.RootCAAnnotation]; root != "" {
			v.field("Root CA", root)
		}
	}

	switch {
	case role.HoldsCertificate():
		for _, key := range []string{"tls.crt", "ca.crt"} {
			v.certificates(key, secrets.SecretData(s, key), now)
		}
		v.field("Private key", presence(len(secrets.SecretData(s, "tls.key")) > 0))
	case role == secrets.RegistryCredentialsRole:
		v.field("Registries", registryHosts(s))
	case role == secrets.GenericRole:
		v.field("Keys", secretKeyNames(s))
	}
}

// invalid prints why the cached entry name is invalid, and the command
// that removes it. For a CA cached with its key by a 4.0.0 alpha release,
// whose removal removes the CA's key, it first says how to keep the CA by
// registering it as the root CA, as the documented migration does.
func (v *secretViewPrinter) invalid(name string, reason *secrets.InvalidCachedSecretError) {
	v.field("Invalid because", reason.Error())
	if reason.Problem == secrets.CachedSecretUnmanagedCAKey {
		v.field("To keep the CA", fmt.Sprintf(`register it as the root CA under a name other than %s before removing it, as "CAs cached by alpha releases" in the Local Environment documentation describes`, name))
		v.field("Register with", "educates local secrets add ca ROOT --cert FILE --key FILE")
	}
	v.field("Remove with", "educates local secrets remove "+name)
}

// registryHosts is the registries the .dockerconfigjson of s holds
// credentials for, sorted and comma separated, or "none". It decodes
// only the registry names, never the credentials.
func registryHosts(s *apiv1.Secret) string {
	var config struct {
		Auths map[string]json.RawMessage `json:"auths"`
	}
	if err := json.Unmarshal(secrets.SecretData(s, apiv1.DockerConfigJsonKey), &config); err != nil || len(config.Auths) == 0 {
		return "none"
	}
	return strings.Join(slices.Sorted(maps.Keys(config.Auths)), ", ")
}

// secretKeyNames is the names of the keys of s, from data and
// stringData, sorted and comma separated, or "none".
func secretKeyNames(s *apiv1.Secret) string {
	keys := map[string]bool{}
	for key := range s.Data {
		keys[key] = true
	}
	for key := range s.StringData {
		keys[key] = true
	}
	if len(keys) == 0 {
		return "none"
	}
	return strings.Join(slices.Sorted(maps.Keys(keys)), ", ")
}

// secretTypeOf is the Kubernetes type of s, which is Opaque when it
// records none, as Kubernetes defaults it.
func secretTypeOf(s *apiv1.Secret) string {
	if s.Type == "" {
		return string(apiv1.SecretTypeOpaque)
	}
	return string(s.Type)
}

// presence is "present" when present is true, else "absent".
func presence(present bool) string {
	if present {
		return "present"
	}
	return "absent"
}

// viewValueColumn is the column values start at, so they line up under
// labels of every length.
const viewValueColumn = 23

// secretViewPrinter collects the labelled lines `local secrets view`
// prints: fields of the Secret, and blocks of indented fields, one for
// each certificate, each under its heading and set off by blank lines.
type secretViewPrinter struct {
	buf bytes.Buffer

	// inBlock is whether the last line printed belongs to a block.
	inBlock bool
}

// field prints a field of the Secret.
func (v *secretViewPrinter) field(label, value string) {
	if v.inBlock {
		v.buf.WriteString("\n")
		v.inBlock = false
	}
	fmt.Fprintf(&v.buf, "%-*s%s\n", viewValueColumn, label+":", value)
}

// block starts a block of fields under heading.
func (v *secretViewPrinter) block(heading string) {
	fmt.Fprintf(&v.buf, "\n%s:\n", heading)
	v.inBlock = true
}

// blockField prints a field of the current block.
func (v *secretViewPrinter) blockField(label, value string) {
	fmt.Fprintf(&v.buf, "%-*s%s\n", viewValueColumn, "  "+label+":", value)
}

// certificates prints a block for each certificate the PEM data of the
// Secret key key holds, in order, or one block saying why none parses.
// It prints nothing when the Secret has no such key.
func (v *secretViewPrinter) certificates(key string, data []byte, now time.Time) {
	if len(data) == 0 {
		return
	}
	certs, err := secrets.ParseCertificatesPEM(data)
	if err != nil {
		v.block(fmt.Sprintf("Certificate (%s)", key))
		v.blockField("Error", err.Error())
		return
	}
	for i, cert := range certs {
		heading := fmt.Sprintf("Certificate (%s)", key)
		if len(certs) > 1 {
			heading = fmt.Sprintf("Certificate (%s, %d of %d)", key, i+1, len(certs))
		}
		v.block(heading)
		v.certificate(cert, now)
	}
}

// certificate prints the fields of cert into the current block.
func (v *secretViewPrinter) certificate(cert *x509.Certificate, now time.Time) {
	if cert.Subject.CommonName != "" {
		v.blockField("Common name", cert.Subject.CommonName)
	}
	if subject := cert.Subject.String(); subject != "" {
		v.blockField("Subject", subject)
	}
	v.blockField("Issuer", cert.Issuer.String())
	if sans := subjectAltNames(cert); sans != "" {
		v.blockField("SANs", sans)
	}
	v.blockField("Valid from", viewTime(cert.NotBefore))
	expires := viewTime(cert.NotAfter)
	if now.After(cert.NotAfter) {
		expires += " (expired)"
	}
	v.blockField("Expires", expires)
	v.blockField("CA", caField(cert))
	if secrets.IsCACertificate(cert) {
		v.blockField("Name constraints", nameConstraints(cert))
	}
	v.blockField("Serial", colonHex(cert.SerialNumber.Bytes()))
	sum := sha256.Sum256(cert.Raw)
	v.blockField("SHA-256 fingerprint", colonHex(sum[:]))
}

// viewTime is t in UTC, to the second.
func viewTime(t time.Time) string {
	return t.UTC().Format(time.DateTime) + " UTC"
}

// subjectAltNames is the DNS names and then the IP addresses of cert,
// comma separated.
func subjectAltNames(cert *x509.Certificate) string {
	sans := append([]string{}, cert.DNSNames...)
	for _, ip := range cert.IPAddresses {
		sans = append(sans, ip.String())
	}
	return strings.Join(sans, ", ")
}

// caField says whether cert is a CA, and for a CA how many CAs may
// follow it in a chain.
func caField(cert *x509.Certificate) string {
	switch {
	case !secrets.IsCACertificate(cert):
		return "no"
	case cert.MaxPathLen > 0 || cert.MaxPathLenZero:
		return fmt.Sprintf("yes, path length %d", cert.MaxPathLen)
	default:
		return "yes, no path length limit"
	}
}

// nameConstraints is the names CA certificate ca permits and excludes,
// or "any" when it has no name constraints.
func nameConstraints(ca *x509.Certificate) string {
	permitted := constrainedNames(ca.PermittedDNSDomains, ca.PermittedIPRanges, ca.PermittedEmailAddresses, ca.PermittedURIDomains)
	excluded := constrainedNames(ca.ExcludedDNSDomains, ca.ExcludedIPRanges, ca.ExcludedEmailAddresses, ca.ExcludedURIDomains)
	var parts []string
	if len(permitted) > 0 {
		parts = append(parts, "permitted: "+strings.Join(permitted, ", "))
	}
	if len(excluded) > 0 {
		parts = append(parts, "excluded: "+strings.Join(excluded, ", "))
	}
	if len(parts) == 0 {
		return "any"
	}
	return strings.Join(parts, "; ")
}

// constrainedNames lists the names of a name constraint: DNS domains,
// then IP ranges, email addresses and URI domains.
func constrainedNames(dns []string, ips []*net.IPNet, emails, uris []string) []string {
	names := append([]string{}, dns...)
	for _, ip := range ips {
		names = append(names, ip.String())
	}
	names = append(names, emails...)
	return append(names, uris...)
}

// colonHex is b as colon separated pairs of uppercase hexadecimal
// digits, "00" when b is empty.
func colonHex(b []byte) string {
	if len(b) == 0 {
		return "00"
	}
	return strings.ReplaceAll(fmt.Sprintf("% X", b), " ", ":")
}

func (p *ProjectInfo) NewLocalSecretsViewCmd() *cobra.Command {
	var o LocalSecretsViewOptions

	var c = &cobra.Command{
		Args:  secretNameArgs,
		Use:   "view NAME",
		Short: "Show the details of a secret in the cache",
		Long: `Show the details of a secret in the local secrets cache that do not fit
the table "educates local secrets list" prints, so you can inspect it
without decoding its YAML. It never prints key material.

Type is the role of the secret and Domains what it is valid for, as
"educates local secrets list" shows them, and Location is the file the
secret is cached in. For a CA intermediate key pair it shows the ingress
domain it was minted for and its root CA.

For each certificate the secret holds, those of tls.crt and then those of
ca.crt, it shows the common name, subject, issuer, subject alternative
names, validity, whether it is a CA and its path length, the name
constraints of a CA, or "any" when it has none, the serial number and the
SHA-256 fingerprint. It shows whether the secret holds a private key,
never the key itself.

For registry credentials it shows only the registries, never the
usernames or passwords, and for a generic secret only the names of its
keys. For an invalid secret it shows why it is invalid and the command
that removes it. For a CA cached with its private key by a 4.0.0 alpha
release, it first shows how to keep the CA by registering it as the root
CA, since removing the secret removes the CA's key.`,
		Example: localSecretsViewExample,
		RunE:    func(cmd *cobra.Command, args []string) error { return o.Run(cmd.OutOrStdout(), args[0]) },
	}

	return c
}
