package packages

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// goldenManifest is the manifest of the golden package source. It is kept
// apart from the manifests other tests use, so that editing those cannot move
// the pinned digests.
const goldenManifest = `apiVersion: packages.educates.dev/v1alpha1
kind: ExtensionPackage
name: golden
version: v1.0.0
description: A package source whose image digests are pinned
`

// goldenTimestamp is the fixed time the golden build stamps on every entry.
var goldenTimestamp = time.Unix(1700000000, 0).UTC()

// goldenLongDir pushes a path past the 100 characters a plain tar header
// holds, so the pinned layer covers an extended header as well.
const goldenLongDir = "common/share/doc/a-directory-name-long-enough-that-its-path-in-the-layer-needs-an-extended-tar-header-record-to-hold-it"

// goldenBinary is deterministic content spanning many tar blocks, so the
// pinned layer covers file content which is copied in more than one piece.
func goldenBinary(seed int) []byte {
	contents := make([]byte, 70000)

	for i := range contents {
		contents[i] = byte((i*31 + seed) % 251)
	}

	return contents
}

// writeGoldenSource builds the golden package source. Every mode is set with
// chmod after the entry is created, so the umask cannot reach the digests,
// and nothing written depends on where the temporary directory is.
func writeGoldenSource(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()

	directories := []string{
		"common/share/empty",
		"common/._cache",
		"linux-arm64/var/empty",
		goldenLongDir,
	}

	for _, name := range directories {
		full := filepath.Join(dir, filepath.FromSlash(name))

		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	files := []struct {
		name     string
		mode     os.FileMode
		contents []byte
	}{
		{"package.yaml", 0o644, []byte(goldenManifest)},
		{"README.md", 0o644, []byte("Left out of the image.\n")},
		{"common/setup.d/01-setup.sh", 0o755, []byte("#!/bin/bash\necho setup\n")},
		{"common/profile.d/01-path.sh", 0o600, []byte("export PATH=$PATH:/opt/golden/bin\n")},
		{"common/bin/tool", 0o644, []byte("the common copy of the tool\n")},
		{"common/bin/group-executable", 0o650, []byte("#!/bin/sh\n")},
		{"common/.keeprc", 0o644, []byte("set -o vi\n")},
		{"common/share/doc/README.md", 0o644, []byte("# Golden\n")},
		{goldenLongDir + "/notes.txt", 0o644, []byte("a file under a long path\n")},
		{"common/.DS_Store", 0o644, []byte("Finder metadata\n")},
		{"common/share/._README.md", 0o644, []byte("AppleDouble metadata\n")},
		{"common/._cache/inside.txt", 0o644, []byte("under a skipped directory\n")},
		{"linux-amd64/bin/tool", 0o755, []byte("the amd64 copy of the tool\n")},
		{"linux-amd64/bin/argocd", 0o755, goldenBinary(1)},
		{"linux-arm64/bin/argocd", 0o700, goldenBinary(2)},
	}

	for _, file := range files {
		full := filepath.Join(dir, filepath.FromSlash(file.name))

		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(full, file.contents, 0o644); err != nil {
			t.Fatal(err)
		}

		if err := os.Chmod(full, file.mode); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// goldenDigests are the compressed digests of one golden build.
type goldenDigests struct {
	index    string
	children map[string]string
}

// goldenLayerTarDigests are the digests of the uncompressed layer of each
// child. They depend on nothing but the package build.
var goldenLayerTarDigests = map[string]string{
	"linux/amd64": "sha256:878cf44efbf2ba1a0e7210f741260980f590f89a3648d58908e7429164b1e0e0",
	"linux/arm64": "sha256:d1baa0f7e4c0eb06cf88a8e6d777613c2f13060cd121e7b5e9bf54b5a45a4b2a",
}

// goldenDigestsByGoRelease holds the child and index digests, which cover the
// gzip-compressed layer. The compressed bytes come from the Go standard
// library's compressor, whose output can change between Go releases, so the
// digests are recorded for each Go release the CLI is built and tested with.
// A Go release which is not listed fails the test, because building the CLI
// with it changes the digests of every published package.
var goldenDigestsByGoRelease = map[string]goldenDigests{
	"go1.26": {
		index: "sha256:4e5c75335daa1e8a2629166e46db5d27bd6f9d9b9232a60aec4f11b016be9b41",
		children: map[string]string{
			"linux/amd64": "sha256:6a90f99c84f4316810f32fb541e407b28c19bcd54bc210291a62e4c46e7209d7",
			"linux/arm64": "sha256:4ea560ce398f77c27a083d3f4276d93d3e7b64d76f90efb83be2cc44e9477683",
		},
	},
	"go1.27": {
		index: "sha256:c2fb8cc68b9cec8683223c6152ec87ba9b350b08f8d1fb89e795868ffdc0e3a8",
		children: map[string]string{
			"linux/amd64": "sha256:858a9e5156211442bfb125225fee9f3adc919f9781f6cbfff8857f9a3d11bb1a",
			"linux/arm64": "sha256:1662cadd862617dfa8b82172a56daf1952628f0898824adf5cea0dcf8ce36b13",
		},
	},
}

// goRelease is the Go release the test runs with, without its patch number,
// such as go1.26.
func goRelease() string {
	parts := strings.SplitN(runtime.Version(), ".", 3)

	if len(parts) < 2 {
		return runtime.Version()
	}

	return parts[0] + "." + parts[1]
}

// Published package images are referenced by tag, and the extension package
// image contract relies on reproducible builds to make that safe: the same
// source must keep giving the same digests. These digests were recorded from
// the package build and must not change.
func TestBuildPackageImage_GoldenDigests(t *testing.T) {
	source := writeGoldenSource(t)

	build, err := BuildPackageImage(source, SupportedPlatforms, goldenTimestamp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Join(build.Ignored, ",") != "README.md" {
		t.Errorf("ignored = %v, want [README.md]", build.Ignored)
	}

	if len(build.Children) != len(goldenLayerTarDigests) {
		t.Fatalf("expected %d children, got %d", len(goldenLayerTarDigests), len(build.Children))
	}

	// The uncompressed layers are checked first: when they hold and the
	// digests below move, the tar is unchanged and the compression is not.
	for _, child := range build.Children {
		want, found := goldenLayerTarDigests[child.Platform.String()]

		if !found {
			t.Fatalf("unexpected child %s", child.Platform)
		}

		layers, err := child.Image.Layers()

		if err != nil {
			t.Fatal(err)
		}

		if len(layers) != 1 {
			t.Fatalf("expected exactly one layer for %s, got %d", child.Platform, len(layers))
		}

		diffID, err := layers[0].DiffID()

		if err != nil {
			t.Fatal(err)
		}

		if diffID.String() != want {
			t.Errorf("the %s layer tar digest is %s, want %s", child.Platform, diffID, want)
		}
	}

	golden, found := goldenDigestsByGoRelease[goRelease()]

	if !found {
		t.Fatalf("no golden digests are recorded for %s, whose compressor may give different package digests: run the test with a recorded Go release, or record this one once the layer tar digests pass", runtime.Version())
	}

	for _, child := range build.Children {
		digest, err := child.Image.Digest()

		if err != nil {
			t.Fatal(err)
		}

		if want := golden.children[child.Platform.String()]; digest.String() != want {
			t.Errorf("the %s child digest is %s, want %s", child.Platform, digest, want)
		}
	}

	index, err := build.Index.Digest()

	if err != nil {
		t.Fatal(err)
	}

	if index.String() != golden.index {
		t.Errorf("the index digest is %s, want %s", index, golden.index)
	}
}
