// Package testregistry runs go-containerregistry's in-process registry for
// tests which publish images, alone or behind a wrapper standing in for what
// a real registry does differently, such as asking for credentials, serving
// TLS with a certificate no system trusts, or failing for a while. It also
// points the Docker configuration a publish reads at a directory the test
// controls.
//
// It is a non-test package so every test suite in the CLI module can import
// it. Production code must not.
package testregistry

import (
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/google/go-containerregistry/pkg/registry"
)

// Start runs the in-process registry over plain http and returns its host.
func Start(t *testing.T) string {
	t.Helper()

	server := httptest.NewServer(registry.New())
	t.Cleanup(server.Close)

	return serverHost(t, server)
}

// Hosts are the two hosts serving the storage of an in-process registry.
type Hosts struct {
	// Publish is the host to publish to, which stands in for the registry
	// under test.
	Publish string

	// Direct serves the same storage over plain http, as it is, for a test
	// to read back what was published.
	Direct string
}

// TLSHosts are the hosts of an in-process registry served over TLS, and the
// certificate it serves.
type TLSHosts struct {
	Hosts

	// CAFile is a PEM file holding the certificate the Publish host serves.
	CAFile string
}

// StartBehind runs the in-process registry behind a wrapper, which the
// Publish host serves.
func StartBehind(t *testing.T, wrap func(http.Handler) http.Handler) Hosts {
	t.Helper()

	storage := registry.New()

	wrapped := httptest.NewServer(wrap(storage))
	t.Cleanup(wrapped.Close)

	direct := httptest.NewServer(storage)
	t.Cleanup(direct.Close)

	return Hosts{Publish: serverHost(t, wrapped), Direct: serverHost(t, direct)}
}

// StartTLS runs the in-process registry over TLS on the Publish host, with a
// certificate no system trusts.
func StartTLS(t *testing.T) TLSHosts {
	t.Helper()

	storage := registry.New()

	secured := httptest.NewTLSServer(storage)
	t.Cleanup(secured.Close)

	direct := httptest.NewServer(storage)
	t.Cleanup(direct.Close)

	caFile := filepath.Join(t.TempDir(), "registry-ca.pem")

	writeFile(t, caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: secured.Certificate().Raw}))

	return TLSHosts{
		Hosts:  Hosts{Publish: serverHost(t, secured), Direct: serverHost(t, direct)},
		CAFile: caFile,
	}
}

// OtherCAFile writes a PEM file holding a certificate authority which signed
// nothing the tests contact.
func OtherCAFile(t *testing.T) string {
	t.Helper()

	ca := testpki.NewRootCA(testpki.WithCommonName("unrelated test CA"))

	path := filepath.Join(t.TempDir(), "other-ca.pem")

	writeFile(t, path, ca.CertPEM)

	return path
}

// RequireAuthorization stands in for a registry which asks for credentials:
// a request without the wanted Authorization header is answered with a
// challenge of the given scheme.
func RequireAuthorization(want string, scheme string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != want {
				w.Header().Set("WWW-Authenticate", fmt.Sprintf("%s realm=\"http://%s/token\",service=\"test\"", scheme, r.Host))
				w.WriteHeader(http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// BasicAuthorization is the Authorization header basic authentication sends.
func BasicAuthorization(username string, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

// RefuseCredentials stands in for a registry which refuses any request
// carrying credentials.
func RefuseCredentials(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// FailFirst stands in for a flaky registry: it answers its first requests
// with 503 Service Unavailable, and serves normally after that.
func FailFirst(failures int) func(http.Handler) http.Handler {
	var requests atomic.Int64

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if requests.Add(1) <= int64(failures) {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// WithoutDockerConfig keeps whatever credentials the machine running the
// tests holds out of a publish.
func WithoutDockerConfig(t *testing.T) {
	t.Helper()

	t.Setenv("DOCKER_CONFIG", t.TempDir())
}

// WithDockerConfig points DOCKER_CONFIG at a Docker configuration holding
// credentials for a host, as docker login would leave them.
func WithDockerConfig(t *testing.T, host string, username string, password string) {
	t.Helper()

	dir := t.TempDir()

	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	config := fmt.Sprintf(`{"auths": {%q: {"auth": %q}}}`, host, auth)

	writeFile(t, filepath.Join(dir, "config.json"), []byte(config))

	t.Setenv("DOCKER_CONFIG", dir)
}

func serverHost(t *testing.T, server *httptest.Server) string {
	t.Helper()

	parsed, err := url.Parse(server.URL)

	if err != nil {
		t.Fatal(err)
	}

	return parsed.Host
}

func writeFile(t *testing.T, path string, contents []byte) {
	t.Helper()

	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}
