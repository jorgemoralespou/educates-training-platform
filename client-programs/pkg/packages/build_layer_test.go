package packages

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// layerNames reads back the single layer of the first child as entry names,
// in the order they are written.
func layerNames(t *testing.T, build *Build) []string {
	t.Helper()

	layers, err := build.Children[0].Image.Layers()

	if err != nil {
		t.Fatal(err)
	}

	reader, err := layers[0].Uncompressed()

	if err != nil {
		t.Fatal(err)
	}

	defer reader.Close()

	var names []string

	tarReader := tar.NewReader(reader)

	for {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		names = append(names, header.Name)
	}

	return names
}

// Entries are sorted by their path, so a directory comes before what it holds
// and a sibling whose name sorts below the separator comes between them.
// Directories end in a slash, the root has no entry, macOS artefacts are
// skipped at any depth, and the manifest comes last.
func TestBuildPackageImage_LayerOrder(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/.keeprc":         0,
		"common/bin/tool":        0o755,
		"common/bin-extra":       0,
		"common/empty/":          0,
		"common/share/doc.txt":   0,
		"common/share/._doc.txt": 0,
		"common/share/.DS_Store": 0,
		"common/zzz/file":        0,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	build, err := BuildPackageImage(source, []Platform{{OS: "linux", Architecture: "amd64"}}, fixedTime())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		".keeprc",
		"bin/",
		"bin-extra",
		"bin/tool",
		"empty/",
		"share/",
		"share/doc.txt",
		"zzz/",
		"zzz/file",
		"package.yaml",
	}

	if got := layerNames(t, build); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("layer entries:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// A named pipe cannot be published, so it is refused and named like a
// symbolic link is.
func TestBuildPackageImage_RejectsNamedPipes(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/bin/tool": 0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := syscall.Mkfifo(filepath.Join(source, "common", "bin", "pipe"), 0o644); err != nil {
		t.Skipf("cannot create a named pipe on this platform: %v", err)
	}

	_, err := BuildPackageImage(source, []Platform{{OS: "linux", Architecture: "amd64"}}, fixedTime())

	if err == nil {
		t.Fatalf("expected a package source containing a named pipe to be refused")
	}

	for _, want := range []string{"common/bin/pipe", "named pipe"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to mention %q, got: %v", want, err)
		}
	}
}

// Building and reading a package image leaves nothing behind in the
// temporary directory.
func TestBuildPackageImage_LeavesNoTemporaryFiles(t *testing.T) {
	source := writeGoldenSource(t)

	temporary := t.TempDir()

	t.Setenv("TMPDIR", temporary)

	build, err := BuildPackageImage(source, SupportedPlatforms, goldenTimestamp)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, child := range build.Children {
		layers, err := child.Image.Layers()

		if err != nil {
			t.Fatal(err)
		}

		reader, err := layers[0].Compressed()

		if err != nil {
			t.Fatal(err)
		}

		if _, err := io.Copy(io.Discard, reader); err != nil {
			t.Fatal(err)
		}

		reader.Close()
	}

	left, err := os.ReadDir(temporary)

	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range left {
		t.Errorf("the build left %s in the temporary directory", entry.Name())
	}
}
