package workshops

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/educates/educates-training-platform/client-programs/internal/testregistry"
	regname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// workshopDefinition names its image through the substitutions publish
// makes, as the workshop templates do.
const workshopDefinition = `apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-example
spec:
  title: Example
  publish:
    image: $(image_repository)/lab-example-files:$(workshop_version)
  workshop:
    files:
    - image:
        url: $(image_repository)/lab-example-files:$(workshop_version)
`

// writeWorkshop writes a workshop directory holding the definition at
// resources/workshop.yaml and the given files, each holding its own path and
// carrying the given mode.
func writeWorkshop(t *testing.T, definition string, files map[string]os.FileMode) string {
	t.Helper()

	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "resources", "workshop.yaml"), definition, 0o644)

	for name, mode := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), name, mode)
	}

	return dir
}

func writeFile(t *testing.T, path string, contents string, mode os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}

	// The mode given to WriteFile is masked by the umask, so set it again.
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// publishWorkshop publishes a workshop directory with the command's defaults
// for whatever the options leave unset, and returns what it printed.
func publishWorkshop(t *testing.T, dir string, options FilesPublishOptions) (string, error) {
	t.Helper()

	if options.WorkshopFile == "" {
		options.WorkshopFile = "resources/workshop.yaml"
	}

	if options.WorkshopVersion == "" {
		options.WorkshopVersion = "latest"
	}

	var stdout bytes.Buffer

	err := options.Publish(&stdout, dir)

	return stdout.String(), err
}

// layerEntry is what a session sees of one entry in a published layer.
type layerEntry struct {
	name     string
	typeflag byte
	mode     int64
	modTime  time.Time
	uid      int
	gid      int
	uname    string
	gname    string
	contents string
}

// pullLayer reads back the single layer of a published image, in order.
func pullLayer(t *testing.T, reference string) []layerEntry {
	t.Helper()

	ref, err := regname.ParseReference(reference)

	if err != nil {
		t.Fatal(err)
	}

	image, err := remote.Image(ref)

	if err != nil {
		t.Fatalf("unable to pull %s: %v", reference, err)
	}

	layers, err := image.Layers()

	if err != nil {
		t.Fatal(err)
	}

	if len(layers) != 1 {
		t.Fatalf("expected one layer, found %d", len(layers))
	}

	reader, err := layers[0].Uncompressed()

	if err != nil {
		t.Fatal(err)
	}

	defer reader.Close()

	var entries []layerEntry

	tarReader := tar.NewReader(reader)

	for {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		contents, err := io.ReadAll(tarReader)

		if err != nil {
			t.Fatal(err)
		}

		entries = append(entries, layerEntry{
			name:     header.Name,
			typeflag: header.Typeflag,
			mode:     header.Mode,
			modTime:  header.ModTime.UTC(),
			uid:      header.Uid,
			gid:      header.Gid,
			uname:    header.Uname,
			gname:    header.Gname,
			contents: string(contents),
		})
	}

	return entries
}

func entryNames(entries []layerEntry) []string {
	names := make([]string, 0, len(entries))

	for _, entry := range entries {
		names = append(names, entry.name)
	}

	return names
}

func assertNames(t *testing.T, entries []layerEntry, want []string) {
	t.Helper()

	got := entryNames(entries)

	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("unexpected layer entries\n got: %q\nwant: %q", got, want)
	}
}

// Without publish.files the whole workshop directory is published, its
// files at the root of the image, and the output names the image and the
// digest it was pushed as.
func TestPublish_WorkshopDirectory(t *testing.T) {
	testregistry.WithoutDockerConfig(t)

	host := testregistry.Start(t)

	dir := writeWorkshop(t, workshopDefinition, map[string]os.FileMode{
		"README.md":                 0o644,
		"workshop/content/index.md": 0o644,
	})

	output, err := publishWorkshop(t, dir, FilesPublishOptions{Repository: host})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reference := host + "/lab-example-files:latest"

	entries := pullLayer(t, reference)

	assertNames(t, entries, []string{
		"README.md",
		"resources/",
		"resources/workshop.yaml",
		"workshop/",
		"workshop/content/",
		"workshop/content/index.md",
	})

	for _, entry := range entries {
		if entry.typeflag == tar.TypeReg && entry.name != "resources/workshop.yaml" && entry.contents != entry.name {
			t.Errorf("%s holds %q", entry.name, entry.contents)
		}
	}

	tag, err := regname.NewTag(reference)

	if err != nil {
		t.Fatal(err)
	}

	descriptor, err := remote.Head(tag)

	if err != nil {
		t.Fatal(err)
	}

	want := fmt.Sprintf("Processing workshop with name \"lab-example\".\n"+
		"Publishing workshop files to %q.\n"+
		"Pushed '%s/lab-example-files@%s'\n"+
		"\n", reference, host, descriptor.Digest)

	if output != want {
		t.Errorf("unexpected output\n got: %q\nwant: %q", output, want)
	}
}

// publishAndPull publishes a workshop directory to a fresh registry and
// reads back the layer of the image it was published as.
func publishAndPull(t *testing.T, dir string) []layerEntry {
	t.Helper()

	testregistry.WithoutDockerConfig(t)

	host := testregistry.Start(t)

	if _, err := publishWorkshop(t, dir, FilesPublishOptions{Repository: host}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return pullLayer(t, host+"/lab-example-files:latest")
}

// Files are executable by everyone when any execute bit is set and readable
// by everyone otherwise, directories are 0755, and everything is owned by
// root, so setup scripts run in a session whatever the author's umask.
func TestPublish_LayerModes(t *testing.T) {
	dir := writeWorkshop(t, workshopDefinition, map[string]os.FileMode{
		"workshop/setup.d/01-setup.sh": 0o755,
		"exercises/run.sh":             0o700,
		"exercises/notes.txt":          0o600,
		"workshop/content/index.md":    0o664,
	})

	entries := publishAndPull(t, dir)

	want := map[string]int64{
		"exercises/":                   0o755,
		"exercises/notes.txt":          0o644,
		"exercises/run.sh":             0o755,
		"resources/":                   0o755,
		"resources/workshop.yaml":      0o644,
		"workshop/":                    0o755,
		"workshop/content/":            0o755,
		"workshop/content/index.md":    0o644,
		"workshop/setup.d/":            0o755,
		"workshop/setup.d/01-setup.sh": 0o755,
	}

	if len(entries) != len(want) {
		t.Errorf("expected %d entries, found %q", len(want), entryNames(entries))
	}

	for _, entry := range entries {
		mode, found := want[entry.name]

		if !found {
			t.Errorf("unexpected entry %s", entry.name)
			continue
		}

		if entry.mode != mode {
			t.Errorf("%s has mode %o, expected %o", entry.name, entry.mode, mode)
		}

		if entry.uid != 0 || entry.gid != 0 || entry.uname != "root" || entry.gname != "root" {
			t.Errorf("%s is owned by %d:%d (%s:%s), expected root", entry.name, entry.uid, entry.gid, entry.uname, entry.gname)
		}
	}
}

// Every entry carries the same timestamp: 2000-01-01 by default, or
// SOURCE_DATE_EPOCH when it is set.
func TestPublish_LayerTimestamps(t *testing.T) {
	cases := []struct {
		name  string
		epoch string
		want  time.Time
	}{
		{
			name: "the default",
			want: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "SOURCE_DATE_EPOCH",
			epoch: "1700000000",
			want:  time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC),
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("SOURCE_DATE_EPOCH", test.epoch)

			dir := writeWorkshop(t, workshopDefinition, map[string]os.FileMode{
				"workshop/content/index.md": 0o644,
			})

			for _, entry := range publishAndPull(t, dir) {
				if !entry.modTime.Equal(test.want) {
					t.Errorf("%s has timestamp %s, expected %s", entry.name, entry.modTime, test.want)
				}
			}
		})
	}
}

// A SOURCE_DATE_EPOCH which is not a positive number of seconds fails the
// publish rather than stamping a time nobody asked for.
func TestPublish_LayerTimestampInvalid(t *testing.T) {
	for _, epoch := range []string{"0", "-1", "yesterday"} {
		t.Run(epoch, func(t *testing.T) {
			t.Setenv("SOURCE_DATE_EPOCH", epoch)

			testregistry.WithoutDockerConfig(t)

			host := testregistry.Start(t)

			dir := writeWorkshop(t, workshopDefinition, nil)

			_, err := publishWorkshop(t, dir, FilesPublishOptions{Repository: host})

			if err == nil {
				t.Fatalf("expected SOURCE_DATE_EPOCH %q to be refused", epoch)
			}

			if !strings.Contains(err.Error(), "SOURCE_DATE_EPOCH") {
				t.Errorf("expected the error to name SOURCE_DATE_EPOCH, got: %v", err)
			}
		})
	}
}

// Entries are sorted by path, so a sibling sorting below the separator falls
// between a directory and its contents. Directory names end in a slash, there
// is no entry for the root, and a directory holding no files is left out.
func TestPublish_LayerOrder(t *testing.T) {
	dir := writeWorkshop(t, workshopDefinition, map[string]os.FileMode{
		"a.txt":                 0o644,
		"a/b.txt":               0o644,
		"a-b/c.txt":             0o644,
		"nested/deeper/file.md": 0o644,
		"Z.md":                  0o644,
	})

	if err := os.MkdirAll(filepath.Join(dir, "empty", "inside"), 0o755); err != nil {
		t.Fatal(err)
	}

	assertNames(t, publishAndPull(t, dir), []string{
		"Z.md",
		"a/",
		"a-b/",
		"a-b/c.txt",
		"a.txt",
		"a/b.txt",
		"nested/",
		"nested/deeper/",
		"nested/deeper/file.md",
		"resources/",
		"resources/workshop.yaml",
	})
}

// Finder's .DS_Store and AppleDouble ._* files are left out at any depth,
// along with a directory holding nothing else. Other dotfiles are kept.
func TestPublish_MacOSArtefactsSkipped(t *testing.T) {
	dir := writeWorkshop(t, workshopDefinition, map[string]os.FileMode{
		".DS_Store":                   0o644,
		"._README.md":                 0o644,
		"README.md":                   0o644,
		".gitignore":                  0o644,
		"workshop/.DS_Store":          0o644,
		"workshop/content/._index.md": 0o644,
		"workshop/content/index.md":   0o644,
		"workshop/content/.hidden":    0o644,
		"only-artefacts/.DS_Store":    0o644,
		"only-artefacts/._notes.txt":  0o644,
	})

	assertNames(t, publishAndPull(t, dir), []string{
		".gitignore",
		"README.md",
		"resources/",
		"resources/workshop.yaml",
		"workshop/",
		"workshop/content/",
		"workshop/content/.hidden",
		"workshop/content/index.md",
	})
}

// Symbolic links and hard-linked files fail the publish, every one of them
// named in the error, and nothing is pushed.
func TestPublish_LinksRefused(t *testing.T) {
	testregistry.WithoutDockerConfig(t)

	host := testregistry.Start(t)

	dir := writeWorkshop(t, workshopDefinition, map[string]os.FileMode{
		"README.md":                 0o644,
		"workshop/content/index.md": 0o644,
		"exercises/original.txt":    0o644,
	})

	if err := os.Symlink("README.md", filepath.Join(dir, "README-link.md")); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("content", filepath.Join(dir, "workshop", "linked-content")); err != nil {
		t.Fatal(err)
	}

	if err := os.Link(filepath.Join(dir, "exercises", "original.txt"), filepath.Join(dir, "exercises", "copy.txt")); err != nil {
		t.Fatal(err)
	}

	_, err := publishWorkshop(t, dir, FilesPublishOptions{Repository: host})

	if err == nil {
		t.Fatalf("expected the links to be refused")
	}

	for _, offender := range []string{
		"README-link.md",
		filepath.Join("workshop", "linked-content"),
		filepath.Join("exercises", "original.txt"),
		filepath.Join("exercises", "copy.txt"),
	} {
		if !strings.Contains(err.Error(), offender) {
			t.Errorf("expected the error to name %s, got: %v", offender, err)
		}
	}

	if strings.Contains(err.Error(), filepath.Join("workshop", "content", "index.md")) {
		t.Errorf("expected the error to name only the links, got: %v", err)
	}

	assertNothingPushed(t, host+"/lab-example-files")
}

// A workshop directory named through a symbolic link is published from where
// the link points: only links inside the content are refused.
func TestPublish_WorkshopDirectoryThroughLink(t *testing.T) {
	dir := writeWorkshop(t, workshopDefinition, map[string]os.FileMode{
		"workshop/content/index.md": 0o644,
	})

	link := filepath.Join(t.TempDir(), "workshop-link")

	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}

	assertNames(t, publishAndPull(t, link), []string{
		"resources/",
		"resources/workshop.yaml",
		"workshop/",
		"workshop/content/",
		"workshop/content/index.md",
	})
}

// assertNothingPushed checks the repository holds no tags.
func assertNothingPushed(t *testing.T, repository string) {
	t.Helper()

	repo, err := regname.NewRepository(repository)

	if err != nil {
		t.Fatal(err)
	}

	if tags, err := remote.List(repo); err == nil && len(tags) != 0 {
		t.Errorf("expected nothing to be pushed, found tags %q", tags)
	}
}

// filesDefinition assembles the content from publish.files, taking the whole
// workshop directory.
const filesDefinition = `apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-example
spec:
  title: Example
  publish:
    image: $(image_repository)/lab-example-files:$(workshop_version)
    files:
    - directory:
        path: .
`

// A .git at the root of the content is never published, whether it is a
// repository's directory or a worktree's file, and with or without
// publish.files. A .git anywhere else is published like any other file.
func TestPublish_GitAtRootLeftOut(t *testing.T) {
	cases := []struct {
		name       string
		definition string
		files      map[string]os.FileMode
		want       []string
	}{
		{
			name:       "a repository without publish.files",
			definition: workshopDefinition,
			files: map[string]os.FileMode{
				".git/config":              0o644,
				".git/objects/ab/cdef":     0o444,
				".gitignore":               0o644,
				"exercises/repo/.git/HEAD": 0o644,
			},
			want: []string{
				".gitignore",
				"exercises/",
				"exercises/repo/",
				"exercises/repo/.git/",
				"exercises/repo/.git/HEAD",
				"resources/",
				"resources/workshop.yaml",
			},
		},
		{
			name:       "a worktree without publish.files",
			definition: workshopDefinition,
			files: map[string]os.FileMode{
				".git":      0o644,
				"README.md": 0o644,
			},
			want: []string{
				"README.md",
				"resources/",
				"resources/workshop.yaml",
			},
		},
		{
			name:       "a repository with publish.files",
			definition: filesDefinition,
			files: map[string]os.FileMode{
				".git/config":              0o644,
				".git/objects/ab/cdef":     0o444,
				".gitignore":               0o644,
				"exercises/repo/.git/HEAD": 0o644,
			},
			want: []string{
				".gitignore",
				"exercises/",
				"exercises/repo/",
				"exercises/repo/.git/",
				"exercises/repo/.git/HEAD",
				"resources/",
				"resources/workshop.yaml",
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			dir := writeWorkshop(t, test.definition, test.files)

			assertNames(t, publishAndPull(t, dir), test.want)
		})
	}
}

// pushedDigest reads the digest a tag names in the registry.
func pushedDigest(t *testing.T, reference string) string {
	t.Helper()

	tag, err := regname.NewTag(reference)

	if err != nil {
		t.Fatal(err)
	}

	descriptor, err := remote.Head(tag)

	if err != nil {
		t.Fatalf("%s was not published: %v", reference, err)
	}

	return descriptor.Digest.String()
}

// Publishing unchanged content twice gives the same digest, however the
// files' own timestamps have moved in between, while changed content gives
// another.
func TestPublish_Reproducible(t *testing.T) {
	testregistry.WithoutDockerConfig(t)

	host := testregistry.Start(t)

	dir := writeWorkshop(t, workshopDefinition, map[string]os.FileMode{
		"workshop/setup.d/01-setup.sh": 0o755,
		"workshop/content/index.md":    0o644,
	})

	publish := func(version string) string {
		t.Helper()

		if _, err := publishWorkshop(t, dir, FilesPublishOptions{Repository: host, WorkshopVersion: version}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		return pushedDigest(t, host+"/lab-example-files:"+version)
	}

	first := publish("1")

	later := time.Now().Add(time.Hour)

	if err := os.Chtimes(filepath.Join(dir, "workshop", "content", "index.md"), later, later); err != nil {
		t.Fatal(err)
	}

	if second := publish("2"); second != first {
		t.Errorf("unchanged content was published as %s, then as %s", first, second)
	}

	writeFile(t, filepath.Join(dir, "workshop", "content", "index.md"), "changed", 0o644)

	if third := publish("3"); third == first {
		t.Errorf("changed content was published with the same digest %s", third)
	}
}

// The workshop image is one OCI image manifest, not an index, with an OCI
// config naming no platform and carrying no labels, and one OCI layer. Only
// the requested tag is written to the repository.
func TestPublish_Image(t *testing.T) {
	testregistry.WithoutDockerConfig(t)

	host := testregistry.Start(t)

	dir := writeWorkshop(t, workshopDefinition, map[string]os.FileMode{
		"workshop/content/index.md": 0o644,
	})

	if _, err := publishWorkshop(t, dir, FilesPublishOptions{Repository: host, WorkshopVersion: "1.0"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tag, err := regname.NewTag(host + "/lab-example-files:1.0")

	if err != nil {
		t.Fatal(err)
	}

	descriptor, err := remote.Get(tag)

	if err != nil {
		t.Fatal(err)
	}

	if descriptor.MediaType != types.OCIManifestSchema1 {
		t.Fatalf("expected an OCI image manifest, found %s", descriptor.MediaType)
	}

	image, err := descriptor.Image()

	if err != nil {
		t.Fatal(err)
	}

	manifest, err := image.Manifest()

	if err != nil {
		t.Fatal(err)
	}

	if manifest.Config.MediaType != types.OCIConfigJSON {
		t.Errorf("expected an OCI image config, found %s", manifest.Config.MediaType)
	}

	if len(manifest.Layers) != 1 {
		t.Fatalf("expected one layer, found %d", len(manifest.Layers))
	}

	if manifest.Layers[0].MediaType != types.OCILayer {
		t.Errorf("expected an OCI gzip layer, found %s", manifest.Layers[0].MediaType)
	}

	if len(manifest.Annotations) != 0 {
		t.Errorf("expected no annotations, found %v", manifest.Annotations)
	}

	config, err := image.ConfigFile()

	if err != nil {
		t.Fatal(err)
	}

	if len(config.Config.Labels) != 0 {
		t.Errorf("expected no labels, found %v", config.Config.Labels)
	}

	if config.OS != "" || config.Architecture != "" || config.Variant != "" {
		t.Errorf("expected no platform, found %s/%s/%s", config.OS, config.Architecture, config.Variant)
	}

	tags, err := remote.List(tag.Context())

	if err != nil {
		t.Fatal(err)
	}

	if fmt.Sprint(tags) != fmt.Sprint([]string{"1.0"}) {
		t.Errorf("expected only the requested tag, found %q", tags)
	}
}

// Publishing leaves the process working directory where it was, whether it
// succeeds or fails, with or without publish.files.
func TestPublish_WorkingDirectoryUnchanged(t *testing.T) {
	cases := []struct {
		name       string
		definition string
		wantErr    bool
	}{
		{
			name:       "the workshop directory",
			definition: workshopDefinition,
		},
		{
			name:       "publish.files",
			definition: filesDefinition,
		},
		{
			name:       "publish.files naming a missing directory",
			definition: strings.Replace(filesDefinition, "path: .", "path: missing", 1),
			wantErr:    true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			testregistry.WithoutDockerConfig(t)

			host := testregistry.Start(t)

			t.Chdir(t.TempDir())

			before, err := os.Getwd()

			if err != nil {
				t.Fatal(err)
			}

			dir := writeWorkshop(t, test.definition, map[string]os.FileMode{
				"workshop/content/index.md": 0o644,
			})

			_, err = publishWorkshop(t, dir, FilesPublishOptions{Repository: host})

			if test.wantErr != (err != nil) {
				t.Fatalf("unexpected result: %v", err)
			}

			after, err := os.Getwd()

			if err != nil {
				t.Fatalf("the working directory is gone: %v", err)
			}

			if after != before {
				t.Errorf("the working directory moved from %s to %s", before, after)
			}
		})
	}
}

// The image name comes from the definition, with $(image_repository) and
// $(workshop_version) substituted, unless an image is given, which wins.
func TestPublish_ImageName(t *testing.T) {
	t.Run("from the definition", func(t *testing.T) {
		testregistry.WithoutDockerConfig(t)

		host := testregistry.Start(t)

		dir := writeWorkshop(t, workshopDefinition, nil)

		output, err := publishWorkshop(t, dir, FilesPublishOptions{Repository: host, WorkshopVersion: "1.2.0"})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		reference := host + "/lab-example-files:1.2.0"

		digest := pushedDigest(t, reference)

		for _, want := range []string{
			fmt.Sprintf("Publishing workshop files to %q.\n", reference),
			fmt.Sprintf("Pushed '%s/lab-example-files@%s'\n", host, digest),
		} {
			if !strings.Contains(output, want) {
				t.Errorf("expected the output to hold %q, got %q", want, output)
			}
		}
	})

	t.Run("given as an option", func(t *testing.T) {
		testregistry.WithoutDockerConfig(t)

		host := testregistry.Start(t)

		dir := writeWorkshop(t, workshopDefinition, nil)

		reference := host + "/elsewhere/other-files:v3"

		output, err := publishWorkshop(t, dir, FilesPublishOptions{Repository: host, Image: reference})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		digest := pushedDigest(t, reference)

		want := fmt.Sprintf("Pushed '%s/elsewhere/other-files@%s'\n", host, digest)

		if !strings.Contains(output, want) {
			t.Errorf("expected the output to hold %q, got %q", want, output)
		}

		assertNothingPushed(t, host+"/lab-example-files")
	})

	t.Run("nowhere", func(t *testing.T) {
		testregistry.WithoutDockerConfig(t)

		host := testregistry.Start(t)

		definition := strings.Replace(workshopDefinition, "    image: $(image_repository)/lab-example-files:$(workshop_version)\n", "", 1)

		dir := writeWorkshop(t, definition, nil)

		_, err := publishWorkshop(t, dir, FilesPublishOptions{Repository: host})

		if err == nil || !strings.Contains(err.Error(), "cannot find image name") {
			t.Fatalf("expected the missing image name to be reported, got: %v", err)
		}
	})
}

// templatedDefinition is processed by ytt, taking its name from a data value.
const templatedDefinition = `#@ load("@ytt:data", "data")
---
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: #@ data.values.name
spec:
  title: #@ "Workshop " + data.values.name
  publish:
    image: $(image_repository)/lab-example-files:$(workshop_version)
`

// The definition is processed by ytt with the data values given before its
// image name is read.
func TestPublish_Templating(t *testing.T) {
	testregistry.WithoutDockerConfig(t)

	host := testregistry.Start(t)

	dir := writeWorkshop(t, templatedDefinition, nil)

	options := FilesPublishOptions{Repository: host}

	options.DataValuesFlags.KVsFromStrings = []string{"name=lab-templated"}

	output, err := publishWorkshop(t, dir, options)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(output, "Processing workshop with name \"lab-templated\".\n") {
		t.Errorf("expected the templated name in the output, got %q", output)
	}

	pushedDigest(t, host+"/lab-example-files:latest")
}

// readExported reads back an exported workshop definition.
func readExported(t *testing.T, path string) map[string]interface{} {
	t.Helper()

	data, err := os.ReadFile(path)

	if err != nil {
		t.Fatalf("the workshop definition was not exported: %v", err)
	}

	var definition map[string]interface{}

	if err := yaml.Unmarshal(data, &definition); err != nil {
		t.Fatal(err)
	}

	return definition
}

// The exported definition carries the substitutions and the version
// published, unless it is latest or the definition names its own, and drops
// the publish section. A relative path is taken from the working directory.
func TestPublish_ExportWorkshop(t *testing.T) {
	cases := []struct {
		name        string
		definition  string
		version     string
		wantVersion string
		wantFound   bool
	}{
		{
			name:        "a version",
			definition:  workshopDefinition,
			version:     "1.2.0",
			wantVersion: "1.2.0",
			wantFound:   true,
		},
		{
			name:       "latest",
			definition: workshopDefinition,
			version:    "latest",
		},
		{
			name:        "a version the definition names",
			definition:  strings.Replace(workshopDefinition, "  title: Example\n", "  title: Example\n  version: \"0.9\"\n", 1),
			version:     "1.2.0",
			wantVersion: "0.9",
			wantFound:   true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			testregistry.WithoutDockerConfig(t)

			host := testregistry.Start(t)

			workingDirectory := t.TempDir()

			t.Chdir(workingDirectory)

			dir := writeWorkshop(t, test.definition, nil)

			_, err := publishWorkshop(t, dir, FilesPublishOptions{
				Repository:      host,
				WorkshopVersion: test.version,
				ExportWorkshop:  "exported.yaml",
			})

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			exported := readExported(t, filepath.Join(workingDirectory, "exported.yaml"))

			version, found, _ := unstructured.NestedString(exported, "spec", "version")

			if found != test.wantFound || version != test.wantVersion {
				t.Errorf("expected the version %q (present %t), got %q (present %t)", test.wantVersion, test.wantFound, version, found)
			}

			if _, found, _ := unstructured.NestedFieldNoCopy(exported, "spec", "publish"); found {
				t.Errorf("expected the publish section to be dropped")
			}

			files, _, _ := unstructured.NestedSlice(exported, "spec", "workshop", "files")

			if len(files) != 1 {
				t.Fatalf("expected one workshop files entry, got %v", files)
			}

			url, _, _ := unstructured.NestedString(files[0].(map[string]interface{}), "image", "url")

			if want := host + "/lab-example-files:" + test.version; url != want {
				t.Errorf("expected the image URL %s, got %s", want, url)
			}
		})
	}
}
