package packages

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/oci"
	regname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// startRegistryBehind runs the in-process registry behind a wrapper standing
// in for what a real registry does differently, such as asking for
// credentials. It returns the host to publish to, and a second host serving
// the same storage directly, from which a test reads back what was published.
func startRegistryBehind(t *testing.T, wrap func(http.Handler) http.Handler) (string, string) {
	t.Helper()

	storage := registry.New()

	wrapped := httptest.NewServer(wrap(storage))
	t.Cleanup(wrapped.Close)

	direct := httptest.NewServer(storage)
	t.Cleanup(direct.Close)

	return serverHost(t, wrapped), serverHost(t, direct)
}

// startTLSRegistry runs the in-process registry over TLS, with a certificate
// no system trusts. It returns the host to publish to, a host serving the
// same storage over http to read back from, and a PEM file holding the
// certificate.
func startTLSRegistry(t *testing.T) (string, string, string) {
	t.Helper()

	storage := registry.New()

	secured := httptest.NewTLSServer(storage)
	t.Cleanup(secured.Close)

	direct := httptest.NewServer(storage)
	t.Cleanup(direct.Close)

	caFile := filepath.Join(t.TempDir(), "registry-ca.pem")

	writeTestFile(t, caFile, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: secured.Certificate().Raw})))

	return serverHost(t, secured), serverHost(t, direct), caFile
}

// otherCAFile writes a PEM file holding a certificate authority which signed
// nothing the tests contact.
func otherCAFile(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	if err != nil {
		t.Fatal(err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "unrelated test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)

	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "other-ca.pem")

	writeTestFile(t, path, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))

	return path
}

func serverHost(t *testing.T, server *httptest.Server) string {
	t.Helper()

	parsed, err := url.Parse(server.URL)

	if err != nil {
		t.Fatal(err)
	}

	return parsed.Host
}

// requireAuthorization stands in for a registry which asks for credentials:
// a request without the wanted Authorization header is answered with a
// challenge of the given scheme.
func requireAuthorization(want string, scheme string) func(http.Handler) http.Handler {
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

func basicAuthorization(username string, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

// refuseCredentials stands in for a registry which refuses any request
// carrying credentials.
func refuseCredentials(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// failFirst stands in for a flaky registry: it answers its first requests
// with 503 Service Unavailable, and serves normally after that.
func failFirst(failures int) func(http.Handler) http.Handler {
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

// withoutDockerConfig keeps whatever credentials the machine running the
// tests holds out of a publish.
func withoutDockerConfig(t *testing.T) {
	t.Helper()

	t.Setenv("DOCKER_CONFIG", t.TempDir())
}

// withDockerConfig points DOCKER_CONFIG at a Docker configuration holding
// credentials for a host, as docker login would leave them.
func withDockerConfig(t *testing.T, host string, username string, password string) {
	t.Helper()

	dir := t.TempDir()

	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))

	config := fmt.Sprintf(`{"auths": {%q: {"auth": %q}}}`, host, auth)

	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DOCKER_CONFIG", dir)
}

// publishTo publishes a package source to a host with the given registry
// options, and returns the file the digest was written to.
func publishTo(t *testing.T, host string, registryOptions oci.RegistryOptions) (string, error) {
	t.Helper()

	digestFile := filepath.Join(t.TempDir(), "digest.txt")

	options := PublishOptions{
		Path:            packageSource(t),
		ImageRepository: host,
		DigestFile:      digestFile,
		Registry:        registryOptions,
	}

	return digestFile, options.Publish(io.Discard)
}

// assertPublished reads the published tag back from the registry's storage
// and checks it names the digest the publish reported.
func assertPublished(t *testing.T, direct string, digestFile string) {
	t.Helper()

	reference, err := regname.NewTag(direct + "/argocd:v2.10.6")

	if err != nil {
		t.Fatal(err)
	}

	descriptor, err := remote.Head(reference)

	if err != nil {
		t.Fatalf("the package image was not published: %v", err)
	}

	reported, err := os.ReadFile(digestFile)

	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(string(reported)) != descriptor.Digest.String() {
		t.Errorf("the tag names %s, but the publish reported %s", descriptor.Digest, strings.TrimSpace(string(reported)))
	}
}

// packageSource writes a small package source to publish.
func packageSource(t *testing.T) string {
	t.Helper()

	source := writeSource(t, map[string]os.FileMode{
		"common/setup.d/01-setup.sh": 0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	return source
}

// A reference given with a scheme is refused, and the error says how to ask
// for plain http instead.
func TestPublishOptions_RegistrySchemeRefused(t *testing.T) {
	host := startRegistry(t)

	cases := []struct {
		name    string
		options PublishOptions
		scheme  string
	}{
		{
			name:    "an http image reference",
			options: PublishOptions{Image: "http://" + host + "/argocd:v2.10.6"},
			scheme:  "http://",
		},
		{
			name:    "an https image repository",
			options: PublishOptions{ImageRepository: "https://" + host},
			scheme:  "https://",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			options := test.options
			options.Path = packageSource(t)

			err := options.Publish(io.Discard)

			if err == nil {
				t.Fatalf("expected a reference with a scheme to be refused")
			}

			for _, want := range []string{test.scheme, "--registry-insecure"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("expected the error to mention %q, got: %v", want, err)
				}
			}
		})
	}
}

// A username gives basic authentication, even with an empty password.
func TestPublishOptions_RegistryBasicAuth(t *testing.T) {
	cases := []struct {
		name     string
		want     string
		registry oci.RegistryOptions
		wantErr  bool
	}{
		{
			name:     "a username and a password",
			want:     basicAuthorization("author", "secret"),
			registry: oci.RegistryOptions{Username: "author", Password: "secret"},
		},
		{
			name:     "a username with an empty password",
			want:     basicAuthorization("author", ""),
			registry: oci.RegistryOptions{Username: "author"},
		},
		{
			name:     "a wrong password",
			want:     basicAuthorization("author", "secret"),
			registry: oci.RegistryOptions{Username: "author", Password: "wrong"},
			wantErr:  true,
		},
		{
			name:    "no credentials",
			want:    basicAuthorization("author", "secret"),
			wantErr: true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			withoutDockerConfig(t)

			host, direct := startRegistryBehind(t, requireAuthorization(test.want, "Basic"))

			digestFile, err := publishTo(t, host, test.registry)

			if test.wantErr {
				if err == nil {
					t.Fatalf("expected the registry to refuse the publish")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertPublished(t, direct, digestFile)
		})
	}
}

// With no credentials given, those docker login left in the Docker
// configuration are used.
func TestPublishOptions_RegistryDockerConfig(t *testing.T) {
	host, direct := startRegistryBehind(t, requireAuthorization(basicAuthorization("author", "secret"), "Basic"))

	withDockerConfig(t, host, "author", "secret")

	digestFile, err := publishTo(t, host, oci.RegistryOptions{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertPublished(t, direct, digestFile)
}

// Anonymous access sends no credentials, even those the Docker configuration
// holds for the registry.
func TestPublishOptions_RegistryAnon(t *testing.T) {
	cases := []struct {
		name     string
		registry oci.RegistryOptions
		wantErr  bool
	}{
		{
			name:     "anonymous access",
			registry: oci.RegistryOptions{Anon: true},
		},
		{
			name:    "the Docker configuration otherwise",
			wantErr: true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			host, direct := startRegistryBehind(t, refuseCredentials)

			withDockerConfig(t, host, "author", "secret")

			digestFile, err := publishTo(t, host, test.registry)

			if test.wantErr {
				if err == nil {
					t.Fatalf("expected the registry to refuse the publish")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertPublished(t, direct, digestFile)
		})
	}
}

// A registry whose certificate no system trusts is reached by trusting its
// certificate authority, or by not verifying certificates at all.
func TestPublishOptions_RegistryTLS(t *testing.T) {
	host, direct, caFile := startTLSRegistry(t)

	otherCAFile := otherCAFile(t)

	cases := []struct {
		name     string
		registry oci.RegistryOptions
		wantErr  bool
	}{
		{
			name:     "its certificate authority trusted",
			registry: oci.RegistryOptions{VerifyCerts: true, CACertPaths: []string{caFile}},
		},
		{
			name:     "its certificate authority trusted beside another",
			registry: oci.RegistryOptions{VerifyCerts: true, CACertPaths: []string{otherCAFile, caFile}},
		},
		{
			name:     "certificates not verified",
			registry: oci.RegistryOptions{VerifyCerts: false},
		},
		{
			name:     "only another certificate authority trusted",
			registry: oci.RegistryOptions{VerifyCerts: true, CACertPaths: []string{otherCAFile}},
			wantErr:  true,
		},
		{
			name:     "only the system certificate authorities trusted",
			registry: oci.RegistryOptions{VerifyCerts: true},
			wantErr:  true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			withoutDockerConfig(t)

			digestFile, err := publishTo(t, host, test.registry)

			if test.wantErr {
				if err == nil {
					t.Fatalf("expected the registry's certificate to be refused")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertPublished(t, direct, digestFile)
		})
	}
}

// A certificate authority file which cannot be read or holds no certificate
// fails the publish rather than being skipped.
func TestPublishOptions_RegistryCACertUnusable(t *testing.T) {
	dir := t.TempDir()

	notACertificate := filepath.Join(dir, "not-a-certificate.pem")

	writeTestFile(t, notACertificate, "not a certificate\n")

	cases := []struct {
		name string
		path string
	}{
		{
			name: "a missing file",
			path: filepath.Join(dir, "absent.pem"),
		},
		{
			name: "a file holding no certificate",
			path: notACertificate,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			withoutDockerConfig(t)

			host := startRegistry(t)

			_, err := publishTo(t, host, oci.RegistryOptions{VerifyCerts: true, CACertPaths: []string{test.path}})

			if err == nil {
				t.Fatalf("expected the certificate authority file to be refused")
			}

			if !strings.Contains(err.Error(), test.path) {
				t.Errorf("expected the error to name %s, got: %v", test.path, err)
			}
		})
	}
}

// The retry count is the number of times a request is tried, zero meaning
// once.
func TestPublishOptions_RegistryRetryCount(t *testing.T) {
	cases := []struct {
		name       string
		retryCount int
		failures   int
		wantErr    bool
	}{
		{
			name:       "three tries outlast two failures",
			retryCount: 3,
			failures:   2,
		},
		{
			name:       "two tries do not outlast two failures",
			retryCount: 2,
			failures:   2,
			wantErr:    true,
		},
		{
			name:       "zero means a single try",
			retryCount: 0,
			failures:   1,
			wantErr:    true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			withoutDockerConfig(t)

			host, direct := startRegistryBehind(t, failFirst(test.failures))

			digestFile, err := publishTo(t, host, oci.RegistryOptions{RetryCount: test.retryCount})

			if test.wantErr {
				if err == nil {
					t.Fatalf("expected the publish to give up")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertPublished(t, direct, digestFile)
		})
	}
}

// Plain http is accepted when asked for. go-containerregistry already allows
// http for a loopback registry, so this only shows the option is accepted.
func TestPublishOptions_RegistryInsecure(t *testing.T) {
	withoutDockerConfig(t)

	host := startRegistry(t)

	digestFile, err := publishTo(t, host, oci.RegistryOptions{Insecure: true})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertPublished(t, host, digestFile)
}

// A token gives bearer authentication, unless a username is also given.
func TestPublishOptions_RegistryToken(t *testing.T) {
	cases := []struct {
		name     string
		registry oci.RegistryOptions
		wantErr  bool
	}{
		{
			name:     "a token",
			registry: oci.RegistryOptions{Token: "issued-token"},
		},
		{
			name:     "a wrong token",
			registry: oci.RegistryOptions{Token: "other-token"},
			wantErr:  true,
		},
		{
			name:     "a username wins over a token",
			registry: oci.RegistryOptions{Username: "author", Password: "secret", Token: "issued-token"},
			wantErr:  true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			withoutDockerConfig(t)

			host, direct := startRegistryBehind(t, requireAuthorization("Bearer issued-token", "Bearer"))

			digestFile, err := publishTo(t, host, test.registry)

			if test.wantErr {
				if err == nil {
					t.Fatalf("expected the registry to refuse the publish")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertPublished(t, direct, digestFile)
		})
	}
}
