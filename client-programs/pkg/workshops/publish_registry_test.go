package workshops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/internal/testregistry"
	"github.com/educates/educates-training-platform/client-programs/pkg/oci"
)

// publishTo publishes a small workshop to a host with the given registry
// options, and returns what the publish printed.
func publishTo(t *testing.T, host string, registryOptions oci.RegistryOptions) (string, error) {
	t.Helper()

	dir := writeWorkshop(t, workshopDefinition, map[string]os.FileMode{
		"workshop/setup.d/01-setup.sh": 0o755,
	})

	return publishWorkshop(t, dir, FilesPublishOptions{Repository: host, Registry: registryOptions})
}

// assertPublished reads the published tag back from the registry's storage
// and checks it names the digest the publish printed.
func assertPublished(t *testing.T, host string, direct string, output string) {
	t.Helper()

	digest := pushedDigest(t, direct+"/lab-example-files:latest")

	want := fmt.Sprintf("Pushed '%s/lab-example-files@%s'\n", host, digest)

	if !strings.Contains(output, want) {
		t.Errorf("expected the output to hold %q, got %q", want, output)
	}
}

// A reference given with a scheme is refused, and the error says how to ask
// for plain http instead.
func TestPublish_RegistrySchemeRefused(t *testing.T) {
	host := testregistry.Start(t)

	cases := []struct {
		name    string
		options FilesPublishOptions
		scheme  string
	}{
		{
			name:    "an http image reference",
			options: FilesPublishOptions{Image: "http://" + host + "/lab-example-files:latest"},
			scheme:  "http://",
		},
		{
			name:    "an https image repository",
			options: FilesPublishOptions{Repository: "https://" + host},
			scheme:  "https://",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			testregistry.WithoutDockerConfig(t)

			dir := writeWorkshop(t, workshopDefinition, nil)

			_, err := publishWorkshop(t, dir, test.options)

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
func TestPublish_RegistryBasicAuth(t *testing.T) {
	cases := []struct {
		name     string
		want     string
		registry oci.RegistryOptions
		wantErr  bool
	}{
		{
			name:     "a username and a password",
			want:     testregistry.BasicAuthorization("author", "secret"),
			registry: oci.RegistryOptions{Username: "author", Password: "secret"},
		},
		{
			name:     "a username with an empty password",
			want:     testregistry.BasicAuthorization("author", ""),
			registry: oci.RegistryOptions{Username: "author"},
		},
		{
			name:     "a wrong password",
			want:     testregistry.BasicAuthorization("author", "secret"),
			registry: oci.RegistryOptions{Username: "author", Password: "wrong"},
			wantErr:  true,
		},
		{
			name:    "no credentials",
			want:    testregistry.BasicAuthorization("author", "secret"),
			wantErr: true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			testregistry.WithoutDockerConfig(t)

			host, direct := testregistry.StartBehind(t, testregistry.RequireAuthorization(test.want, "Basic"))

			output, err := publishTo(t, host, test.registry)

			if test.wantErr {
				if err == nil {
					t.Fatalf("expected the registry to refuse the publish")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertPublished(t, host, direct, output)
		})
	}
}

// With no credentials given, those docker login left in the Docker
// configuration are used.
func TestPublish_RegistryDockerConfig(t *testing.T) {
	host, direct := testregistry.StartBehind(t, testregistry.RequireAuthorization(testregistry.BasicAuthorization("author", "secret"), "Basic"))

	testregistry.WithDockerConfig(t, host, "author", "secret")

	output, err := publishTo(t, host, oci.RegistryOptions{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertPublished(t, host, direct, output)
}

// Anonymous access sends no credentials, even those the Docker configuration
// holds for the registry.
func TestPublish_RegistryAnon(t *testing.T) {
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
			host, direct := testregistry.StartBehind(t, testregistry.RefuseCredentials)

			testregistry.WithDockerConfig(t, host, "author", "secret")

			output, err := publishTo(t, host, test.registry)

			if test.wantErr {
				if err == nil {
					t.Fatalf("expected the registry to refuse the publish")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertPublished(t, host, direct, output)
		})
	}
}

// A registry whose certificate no system trusts is reached by trusting its
// certificate authority, or by not verifying certificates at all.
func TestPublish_RegistryTLS(t *testing.T) {
	host, direct, caFile := testregistry.StartTLS(t)

	otherCAFile := testregistry.OtherCAFile(t)

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
			testregistry.WithoutDockerConfig(t)

			output, err := publishTo(t, host, test.registry)

			if test.wantErr {
				if err == nil {
					t.Fatalf("expected the registry's certificate to be refused")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertPublished(t, host, direct, output)
		})
	}
}

// A certificate authority file which cannot be read or holds no certificate
// fails the publish rather than being skipped.
func TestPublish_RegistryCACertUnusable(t *testing.T) {
	dir := t.TempDir()

	notACertificate := filepath.Join(dir, "not-a-certificate.pem")

	writeFile(t, notACertificate, "not a certificate\n", 0o644)

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
			testregistry.WithoutDockerConfig(t)

			host := testregistry.Start(t)

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
func TestPublish_RegistryRetryCount(t *testing.T) {
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
			testregistry.WithoutDockerConfig(t)

			host, direct := testregistry.StartBehind(t, testregistry.FailFirst(test.failures))

			output, err := publishTo(t, host, oci.RegistryOptions{RetryCount: test.retryCount})

			if test.wantErr {
				if err == nil {
					t.Fatalf("expected the publish to give up")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertPublished(t, host, direct, output)
		})
	}
}

// Plain http is accepted when asked for. go-containerregistry already allows
// http for a loopback registry, so this only shows the option is accepted.
func TestPublish_RegistryInsecure(t *testing.T) {
	testregistry.WithoutDockerConfig(t)

	host := testregistry.Start(t)

	output, err := publishTo(t, host, oci.RegistryOptions{Insecure: true})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assertPublished(t, host, host, output)
}

// A token gives bearer authentication, unless a username is also given.
func TestPublish_RegistryToken(t *testing.T) {
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
			testregistry.WithoutDockerConfig(t)

			host, direct := testregistry.StartBehind(t, testregistry.RequireAuthorization("Bearer issued-token", "Bearer"))

			output, err := publishTo(t, host, test.registry)

			if test.wantErr {
				if err == nil {
					t.Fatalf("expected the registry to refuse the publish")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			assertPublished(t, host, direct, output)
		})
	}
}
