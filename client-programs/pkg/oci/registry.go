package oci

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	regname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/pkg/errors"
)

// RegistryOptions say how the CLI reaches an image registry: the certificate
// authorities it trusts, whether plain http is allowed, the credentials it
// presents, and how long and how often it tries. A command binds each field
// to one of its --registry-* flags.
type RegistryOptions struct {
	// CACertPaths are PEM files of certificate authorities trusted in
	// addition to the system ones.
	CACertPaths []string

	// VerifyCerts checks the registry's certificate chain and host name. The
	// zero value skips the check, so a command binds it with a default of
	// true.
	VerifyCerts bool

	// Insecure allows plain http.
	Insecure bool

	// Username and Password give basic authentication.
	Username string
	Password string

	// Token gives bearer authentication.
	Token string

	// Anon sends no credentials at all.
	Anon bool

	// ResponseHeaderTimeout bounds the wait for a response's headers. Zero
	// means no limit.
	ResponseHeaderTimeout time.Duration

	// RetryCount is the number of times a request is tried, zero or less
	// meaning once. The wait between tries starts at 100ms and doubles, up to
	// a second.
	RetryCount int
}

// ParseTag parses an image reference naming a tag. A reference starting with
// a scheme is refused: plain http is asked for with Insecure instead.
func (o *RegistryOptions) ParseTag(value string) (regname.Tag, error) {
	for _, scheme := range []string{"http://", "https://"} {
		if strings.HasPrefix(value, scheme) {
			return regname.Tag{}, errors.Errorf("the image reference %q must not start with %s: give it without the scheme, and use --registry-insecure for a registry served over http", value, scheme)
		}
	}

	var options []regname.Option

	if o.Insecure {
		options = append(options, regname.Insecure)
	}

	tag, err := regname.NewTag(value, options...)

	if err != nil {
		return regname.Tag{}, errors.Wrapf(err, "unable to parse the image reference %q", value)
	}

	return tag, nil
}

// RemoteOptions turns the options into go-containerregistry remote options.
func (o *RegistryOptions) RemoteOptions() ([]remote.Option, error) {
	transport, err := o.transport()

	if err != nil {
		return nil, err
	}

	tries := o.RetryCount

	if tries < 1 {
		tries = 1
	}

	return []remote.Option{
		remote.WithTransport(transport),
		remote.WithRetryBackoff(remote.Backoff{
			Duration: 100 * time.Millisecond,
			Factor:   2,
			Steps:    tries,
			Cap:      time.Second,
		}),
		o.authOption(),
	}, nil
}

// transport starts from Go's default transport, so the proxy environment
// variables apply, and adds the certificate authorities and the timeout.
func (o *RegistryOptions) transport() (*http.Transport, error) {
	pool, err := x509.SystemCertPool()

	if err != nil {
		return nil, errors.Wrap(err, "unable to load the system certificate authorities")
	}

	for _, path := range o.CACertPaths {
		certificates, err := os.ReadFile(path)

		if err != nil {
			return nil, errors.Wrapf(err, "unable to read the CA certificates in %s", path)
		}

		if !pool.AppendCertsFromPEM(certificates) {
			return nil, errors.Errorf("no PEM encoded CA certificate found in %s", path)
		}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()

	// HTTP/2 is not forced, so with a TLS configuration of its own the
	// transport speaks HTTP/1.1.
	transport.ForceAttemptHTTP2 = false
	transport.ResponseHeaderTimeout = o.ResponseHeaderTimeout
	transport.TLSClientConfig = &tls.Config{
		RootCAs:            pool,
		InsecureSkipVerify: !o.VerifyCerts,
	}

	return transport, nil
}

// authOption picks the credentials, the first match winning.
func (o *RegistryOptions) authOption() remote.Option {
	switch {
	case o.Username != "":
		// go-containerregistry sends a username and password only when both
		// are set, so the encoded form carries a username whose password is
		// empty.
		return remote.WithAuth(authn.FromConfig(authn.AuthConfig{
			Username: o.Username,
			Password: o.Password,
			Auth:     base64.StdEncoding.EncodeToString([]byte(o.Username + ":" + o.Password)),
		}))
	case o.Token != "":
		return remote.WithAuth(&authn.Bearer{Token: o.Token})
	case o.Anon:
		return remote.WithAuth(authn.Anonymous)
	default:
		return remote.WithAuthFromKeychain(authn.DefaultKeychain)
	}
}
