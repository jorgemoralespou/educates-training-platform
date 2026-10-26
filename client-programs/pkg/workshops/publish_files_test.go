package workshops

import (
	"archive/tar"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/internal/testregistry"
)

// publishFiles returns a workshop definition whose publish.files section
// holds the given entries, written as they would sit under files:.
func publishFiles(entries string) string {
	var section strings.Builder

	for _, line := range strings.Split(strings.Trim(entries, "\n"), "\n") {
		section.WriteString("    " + line + "\n")
	}

	return `apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-example
spec:
  title: Example
  publish:
    image: $(image_repository)/lab-example-files:$(workshop_version)
    files:
` + section.String()
}

// publishRefused publishes a workshop directory expecting the publish to
// fail without pushing anything, and returns the error.
func publishRefused(t *testing.T, dir string) error {
	t.Helper()

	testregistry.WithoutDockerConfig(t)

	host := testregistry.Start(t)

	_, err := publishWorkshop(t, dir, FilesPublishOptions{Repository: host})

	if err == nil {
		t.Fatalf("expected the publish to fail")
	}

	assertNothingPushed(t, host+"/lab-example-files")

	return err
}

func assertErrorHolds(t *testing.T, err error, wants ...string) {
	t.Helper()

	for _, want := range wants {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to hold %q, got: %v", want, err)
		}
	}
}

// fileNames lists the files in a layer, leaving out its directories.
func fileNames(entries []layerEntry) []string {
	names := []string{}

	for _, entry := range entries {
		if entry.typeflag == tar.TypeReg {
			names = append(names, entry.name)
		}
	}

	return names
}

// An entry naming a remote source fails the publish, naming the entry's
// position and its source type, and saying where the files belong now.
func TestPublishFiles_RemoteSourcesRefused(t *testing.T) {
	for _, source := range []string{
		"git",
		"hg",
		"http",
		"image",
		"imgpkgBundle",
		"githubRelease",
		"helmChart",
		"manual",
		"inline",
	} {
		t.Run(source, func(t *testing.T) {
			dir := writeWorkshop(t, publishFiles(`
- directory:
    path: .
- path: extra
  `+source+`:
    url: https://example.com/files
`), map[string]os.FileMode{
				"workshop/content/index.md": 0o644,
			})

			err := publishRefused(t, dir)

			assertErrorHolds(t, err,
				"spec.publish.files[1]",
				`"`+source+`"`,
				"remote sources were removed from publish.files in 4.0.0",
				"workshop directory",
			)
		})
	}
}

// Symbolic links and hard-linked files in what an entry selects fail the
// publish, every one of them named in the error.
func TestPublishFiles_LinksRefused(t *testing.T) {
	dir := writeWorkshop(t, publishFiles(`
- directory:
    path: .
`), map[string]os.FileMode{
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

	err := publishRefused(t, dir)

	assertErrorHolds(t, err,
		"README-link.md",
		filepath.Join("workshop", "linked-content"),
		filepath.Join("exercises", "original.txt"),
		filepath.Join("exercises", "copy.txt"),
	)

	if strings.Contains(err.Error(), "index.md") {
		t.Errorf("expected the error to name only the links, got: %v", err)
	}
}

// Links an entry does not select are no obstacle, and the directory an entry
// takes its files from may itself be named through a symbolic link.
func TestPublishFiles_LinksNotSelected(t *testing.T) {
	dir := writeWorkshop(t, publishFiles(`
- path: workshop
  directory:
    path: linked-workshop
  excludePaths:
  - /content/index-link.md
  - /exercises/copy.txt
`), map[string]os.FileMode{
		"workshop/content/index.md": 0o644,
		"original.txt":              0o644,
	})

	if err := os.Symlink("workshop", filepath.Join(dir, "linked-workshop")); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("index.md", filepath.Join(dir, "workshop", "content", "index-link.md")); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "workshop", "exercises"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Link(filepath.Join(dir, "original.txt"), filepath.Join(dir, "workshop", "exercises", "copy.txt")); err != nil {
		t.Fatal(err)
	}

	assertFiles(t, fileNames(publishAndPull(t, dir)), []string{
		"workshop/content/index.md",
	})
}

// An entry places its files under its path in the image, or at the image
// root without one. A relative directory.path is taken from the workshop
// directory, whatever the working directory, and an absolute one as it is;
// either may lie outside the workshop directory.
func TestPublishFiles_EntryPaths(t *testing.T) {
	shared := t.TempDir()

	writeFile(t, filepath.Join(shared, "notes.txt"), "notes.txt", 0o644)

	dir := writeWorkshop(t, workshopDefinition, map[string]os.FileMode{
		"README.md":                 0o644,
		"workshop/content/index.md": 0o644,
	})

	relativeShared, err := filepath.Rel(dir, shared)

	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(dir, "resources", "workshop.yaml"), publishFiles(`
- directory:
    path: workshop/content
- path: /workshop
  directory:
    path: workshop
- path: extras/absolute
  directory:
    path: `+shared+`
- path: extras/relative/
  directory:
    path: `+filepath.ToSlash(relativeShared)+`
`), 0o644)

	t.Chdir(t.TempDir())

	assertNames(t, publishAndPull(t, dir), []string{
		"extras/",
		"extras/absolute/",
		"extras/absolute/notes.txt",
		"extras/relative/",
		"extras/relative/notes.txt",
		"index.md",
		"workshop/",
		"workshop/content/",
		"workshop/content/index.md",
	})
}

// publishFilesAndList publishes a workshop directory holding the given files,
// whose definition has the given publish.files entries, and lists the files
// in the published layer.
func publishFilesAndList(t *testing.T, entries string, files map[string]os.FileMode) []string {
	t.Helper()

	return fileNames(publishAndPull(t, writeWorkshop(t, publishFiles(entries), files)))
}

func assertFiles(t *testing.T, got []string, want []string) {
	t.Helper()

	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("unexpected files\n got: %q\nwant: %q", got, want)
	}
}

// A file is kept when it matches an includePaths pattern, or there are none,
// and matches no excludePaths pattern. Patterns are matched against files
// only, by their path from the content root, where a leading slash is the
// root and a trailing slash is dropped.
func TestPublishFiles_IncludeAndExcludePaths(t *testing.T) {
	files := map[string]os.FileMode{
		"README.md":                 0o644,
		"workshop/content/index.md": 0o644,
		"workshop/content/draft.md": 0o644,
		"exercises/run.sh":          0o755,
		"exercises/notes.txt":       0o644,
		"other/file.txt":            0o644,
	}

	cases := []struct {
		name    string
		entries string
		want    []string
	}{
		{
			name: "includePaths from the workshop templates",
			entries: `
- directory:
    path: .
  includePaths:
  - /workshop/**
  - /exercises/**
  - /README.md
`,
			want: []string{
				"README.md",
				"exercises/notes.txt",
				"exercises/run.sh",
				"workshop/content/draft.md",
				"workshop/content/index.md",
			},
		},
		{
			name: "excludePaths alone",
			entries: `
- directory:
    path: .
  excludePaths:
  - /workshop/content/draft.md
  - other/**
`,
			want: []string{
				"README.md",
				"exercises/notes.txt",
				"exercises/run.sh",
				"resources/workshop.yaml",
				"workshop/content/index.md",
			},
		},
		{
			name: "excludePaths winning over includePaths",
			entries: `
- directory:
    path: .
  includePaths:
  - /workshop/**
  excludePaths:
  - '**/draft.md'
`,
			want: []string{
				"workshop/content/index.md",
			},
		},
		{
			name: "a trailing slash",
			entries: `
- directory:
    path: .
  includePaths:
  - /exercises/**/
`,
			want: []string{
				"exercises/notes.txt",
				"exercises/run.sh",
			},
		},
		{
			name: "a pattern naming a directory",
			entries: `
- directory:
    path: .
  includePaths:
  - /workshop
  - /README.md
`,
			want: []string{
				"README.md",
			},
		},
		{
			name: "patterns from a source directory's own root",
			entries: `
- path: workshop
  directory:
    path: workshop
  includePaths:
  - /content/index.md
`,
			want: []string{
				"workshop/content/index.md",
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			assertFiles(t, publishFilesAndList(t, test.entries, files), test.want)
		})
	}
}

// The licence, copyright and notice files at the content root are kept
// whatever includePaths and excludePaths say, unless legalPaths names other
// patterns to keep in their place, or none.
func TestPublishFiles_LegalPaths(t *testing.T) {
	files := map[string]os.FileMode{
		"LICENSE":                   0o644,
		"Licence.rst":               0o644,
		"COPYRIGHT.txt":             0o644,
		"NOTICE.md":                 0o644,
		"license.txt":               0o644,
		"LICENSE.pdf":               0o644,
		"docs/LICENSE":              0o644,
		"README.md":                 0o644,
		"workshop/content/index.md": 0o644,
	}

	cases := []struct {
		name    string
		entries string
		want    []string
	}{
		{
			name: "the defaults against includePaths",
			entries: `
- directory:
    path: .
  includePaths:
  - /workshop/**
`,
			want: []string{
				"COPYRIGHT.txt",
				"LICENSE",
				"Licence.rst",
				"NOTICE.md",
				"workshop/content/index.md",
			},
		},
		{
			name: "the defaults against excludePaths",
			entries: `
- directory:
    path: .
  excludePaths:
  - '*'
  - /resources/**
`,
			want: []string{
				"COPYRIGHT.txt",
				"LICENSE",
				"Licence.rst",
				"NOTICE.md",
				"docs/LICENSE",
				"workshop/content/index.md",
			},
		},
		{
			name: "legalPaths given",
			entries: `
- directory:
    path: .
  includePaths:
  - /workshop/**
  legalPaths:
  - '**/LICENSE'
`,
			want: []string{
				"LICENSE",
				"docs/LICENSE",
				"workshop/content/index.md",
			},
		},
		{
			name: "legalPaths empty",
			entries: `
- directory:
    path: .
  includePaths:
  - /workshop/**
  legalPaths: []
`,
			want: []string{
				"workshop/content/index.md",
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			assertFiles(t, publishFilesAndList(t, test.entries, files), test.want)
		})
	}
}

// newRootPath makes a directory the root of an entry's content once the
// patterns, which are matched from the original root, have selected its
// files. Everything outside it is dropped, legal files included.
func TestPublishFiles_NewRootPath(t *testing.T) {
	files := map[string]os.FileMode{
		"LICENSE":                   0o644,
		"README.md":                 0o644,
		"workshop/content/index.md": 0o644,
		"workshop/content/draft.md": 0o644,
		"workshop/setup.d/setup.sh": 0o755,
	}

	cases := []struct {
		name    string
		entries string
		want    []string
	}{
		{
			name: "after filtering",
			entries: `
- path: files
  directory:
    path: .
  includePaths:
  - /workshop/content/**
  excludePaths:
  - '**/draft.md'
  newRootPath: workshop
`,
			want: []string{
				"files/content/index.md",
			},
		},
		{
			name: "with leading and trailing slashes",
			entries: `
- directory:
    path: .
  newRootPath: /workshop/content/
`,
			want: []string{
				"draft.md",
				"index.md",
			},
		},
		{
			name: "naming the content root",
			entries: `
- directory:
    path: workshop
  newRootPath: .
`,
			want: []string{
				"content/draft.md",
				"content/index.md",
				"setup.d/setup.sh",
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			assertFiles(t, publishFilesAndList(t, test.entries, files), test.want)
		})
	}
}

// A newRootPath which is not a directory in the content, or which leaves it,
// fails the publish, naming the entry.
func TestPublishFiles_NewRootPathRefused(t *testing.T) {
	cases := []struct {
		name        string
		newRootPath string
		want        string
	}{
		{name: "missing", newRootPath: "missing", want: "does not exist"},
		{name: "a file", newRootPath: "README.md", want: "is not a directory"},
		{name: "outside the content", newRootPath: "../elsewhere", want: "outside the content"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			dir := writeWorkshop(t, publishFiles(`
- directory:
    path: .
  newRootPath: `+test.newRootPath+`
`), map[string]os.FileMode{
				"README.md":                 0o644,
				"workshop/content/index.md": 0o644,
			})

			assertErrorHolds(t, publishRefused(t, dir), "spec.publish.files[0]", "newRootPath", test.newRootPath, test.want)
		})
	}
}

// An entry left with no files fails the publish, naming the entry, however
// its files went: never there, not selected, or outside its newRootPath.
func TestPublishFiles_EntryWithoutFilesRefused(t *testing.T) {
	cases := []struct {
		name  string
		entry string
	}{
		{
			name: "an empty directory",
			entry: `
- path: extra
  directory:
    path: empty
`,
		},
		{
			name: "a directory holding only macOS artefacts",
			entry: `
- path: extra
  directory:
    path: artefacts
`,
		},
		{
			name: "a mistyped pattern",
			entry: `
- path: extra
  directory:
    path: .
  includePaths:
  - /workshop/contnet/**
`,
		},
		{
			name: "a newRootPath outside what is selected",
			entry: `
- path: extra
  directory:
    path: .
  includePaths:
  - /workshop/content/**
  newRootPath: exercises
`,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			dir := writeWorkshop(t, publishFiles(`
- directory:
    path: .
`+test.entry), map[string]os.FileMode{
				"workshop/content/index.md": 0o644,
				"exercises/run.sh":          0o755,
				"artefacts/.DS_Store":       0o644,
				"artefacts/._notes.txt":     0o644,
			})

			if err := os.MkdirAll(filepath.Join(dir, "empty", "inside"), 0o755); err != nil {
				t.Fatal(err)
			}

			assertErrorHolds(t, publishRefused(t, dir), "spec.publish.files[1]", "no files")
		})
	}
}

// Entries apply in order, each first removing what earlier entries placed at
// or under its path, so a later entry replaces a subtree of an earlier one,
// a later entry at an ancestor replaces all of an earlier one's files, and of
// two entries at the same path the last wins.
func TestPublishFiles_OverlappingEntries(t *testing.T) {
	files := map[string]os.FileMode{
		"workshop/content/index.md": 0o644,
		"workshop/setup.d/setup.sh": 0o755,
		"override/other.md":         0o644,
	}

	cases := []struct {
		name    string
		entries string
		want    []string
	}{
		{
			name: "a subtree after its ancestor",
			entries: `
- path: workshop
  directory:
    path: workshop
- path: workshop/content
  directory:
    path: override
`,
			want: []string{
				"workshop/content/other.md",
				"workshop/setup.d/setup.sh",
			},
		},
		{
			name: "an ancestor after its subtree",
			entries: `
- path: workshop/content
  directory:
    path: override
- path: workshop
  directory:
    path: workshop
`,
			want: []string{
				"workshop/content/index.md",
				"workshop/setup.d/setup.sh",
			},
		},
		{
			name: "the same path twice",
			entries: `
- path: workshop/content
  directory:
    path: workshop/content
- path: /workshop/content/
  directory:
    path: override
`,
			want: []string{
				"workshop/content/other.md",
			},
		},
		{
			name: "the image root last",
			entries: `
- path: extra
  directory:
    path: override
- directory:
    path: workshop
`,
			want: []string{
				"content/index.md",
				"setup.d/setup.sh",
			},
		},
		{
			name: "siblings",
			entries: `
- path: extra/one
  directory:
    path: override
- path: extra/two
  directory:
    path: override
`,
			want: []string{
				"extra/one/other.md",
				"extra/two/other.md",
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			assertFiles(t, publishFilesAndList(t, test.entries, files), test.want)
		})
	}
}

// An entry whose path lies under a file an earlier entry placed fails the
// publish, naming the entry and the file.
func TestPublishFiles_PathUnderFileRefused(t *testing.T) {
	dir := writeWorkshop(t, publishFiles(`
- directory:
    path: workshop
- path: setup.d/setup.sh/more
  directory:
    path: override
`), map[string]os.FileMode{
		"workshop/content/index.md": 0o644,
		"workshop/setup.d/setup.sh": 0o755,
		"override/other.md":         0o644,
	})

	assertErrorHolds(t, publishRefused(t, dir), "spec.publish.files[1]", `"setup.d/setup.sh"`)
}

// lazy, ignorePaths, permissions and keys publishing does not know change
// nothing in the image.
func TestPublishFiles_IgnoredFields(t *testing.T) {
	dir := writeWorkshop(t, publishFiles(`
- path: workshop
  directory:
    path: workshop
    unknown: value
  lazy: true
  ignorePaths:
  - /content/**
  permissions: 0700
  unknown:
    nested: value
`), map[string]os.FileMode{
		"workshop/content/index.md": 0o644,
		"workshop/setup.d/setup.sh": 0o755,
	})

	entries := publishAndPull(t, dir)

	assertNames(t, entries, []string{
		"workshop/",
		"workshop/content/",
		"workshop/content/index.md",
		"workshop/setup.d/",
		"workshop/setup.d/setup.sh",
	})

	want := map[string]int64{
		"workshop/":                 0o755,
		"workshop/content/":         0o755,
		"workshop/content/index.md": 0o644,
		"workshop/setup.d/":         0o755,
		"workshop/setup.d/setup.sh": 0o755,
	}

	for _, entry := range entries {
		if entry.mode != want[entry.name] {
			t.Errorf("%s has mode %o, expected %o", entry.name, entry.mode, want[entry.name])
		}
	}
}

// An entry naming no directory to take its files from fails the publish,
// naming the entry.
func TestPublishFiles_EntryWithoutDirectoryRefused(t *testing.T) {
	for _, entry := range []string{
		"- path: extra",
		"- path: extra\n  directory: {}",
		"- path: extra\n  directory:\n    path: ''",
	} {
		t.Run(entry, func(t *testing.T) {
			dir := writeWorkshop(t, publishFiles(entry), map[string]os.FileMode{
				"workshop/content/index.md": 0o644,
			})

			assertErrorHolds(t, publishRefused(t, dir), "spec.publish.files[0]", "directory.path")
		})
	}
}

// The pattern syntax publish.files supports is doublestar v4.10.4's. A bump
// of the library which changes what a pattern matches fails here before it
// reaches workshop authors. Among what the cases pin, [!x] negates the class
// as [^x] does, rather than naming ! and x, and dir/** matches a file named
// dir as well as everything under a directory of that name.
func TestPublishFiles_PatternSyntax(t *testing.T) {
	files := map[string]os.FileMode{}

	for _, name := range []string{
		".hidden",
		"README.md",
		"build",
		"q1.md",
		"q?.md",
		"star*.md",
		"starry.md",
		"docs/.draft.md",
		"docs/guide.md",
		"docs/notes.txt",
		"docs/deep/more.md",
		"docs/deep/deeper/most.md",
		"exercises/run.sh",
		"workshop/content/!.md",
		"workshop/content/a.md",
		"workshop/content/b.md",
		"workshop/content/c.md",
		"workshop/content/index.md",
	} {
		files["content/"+name] = 0o644
	}

	cases := []struct {
		name     string
		patterns []string
		want     []string
	}{
		{
			name:     "the workshop template",
			patterns: []string{"/workshop/**", "/exercises/**", "/README.md"},
			want: []string{
				"README.md",
				"exercises/run.sh",
				"workshop/content/!.md",
				"workshop/content/a.md",
				"workshop/content/b.md",
				"workshop/content/c.md",
				"workshop/content/index.md",
			},
		},
		{
			name:     "** at the start",
			patterns: []string{"**/*.md"},
			want: []string{
				"README.md",
				"docs/.draft.md",
				"docs/deep/deeper/most.md",
				"docs/deep/more.md",
				"docs/guide.md",
				"q1.md",
				"q?.md",
				"star*.md",
				"starry.md",
				"workshop/content/!.md",
				"workshop/content/a.md",
				"workshop/content/b.md",
				"workshop/content/c.md",
				"workshop/content/index.md",
			},
		},
		{
			name:     "** in the middle",
			patterns: []string{"docs/**/more.md"},
			want:     []string{"docs/deep/more.md"},
		},
		{
			name:     "** in the middle matching no directory",
			patterns: []string{"docs/**/*.md"},
			want: []string{
				"docs/.draft.md",
				"docs/deep/deeper/most.md",
				"docs/deep/more.md",
				"docs/guide.md",
			},
		},
		{
			name:     "** at the end",
			patterns: []string{"docs/**"},
			want: []string{
				"docs/.draft.md",
				"docs/deep/deeper/most.md",
				"docs/deep/more.md",
				"docs/guide.md",
				"docs/notes.txt",
			},
		},
		{
			name:     "** alone",
			patterns: []string{"**"},
			want: []string{
				".hidden",
				"README.md",
				"build",
				"docs/.draft.md",
				"docs/deep/deeper/most.md",
				"docs/deep/more.md",
				"docs/guide.md",
				"docs/notes.txt",
				"exercises/run.sh",
				"q1.md",
				"q?.md",
				"star*.md",
				"starry.md",
				"workshop/content/!.md",
				"workshop/content/a.md",
				"workshop/content/b.md",
				"workshop/content/c.md",
				"workshop/content/index.md",
			},
		},
		{
			name:     "** within a name acting as *",
			patterns: []string{"docs/**.md"},
			want:     []string{"docs/.draft.md", "docs/guide.md"},
		},
		{
			name:     "dir/** against a file named dir",
			patterns: []string{"build/**"},
			want:     []string{"build"},
		},
		{
			name:     "* within one directory",
			patterns: []string{"*.md"},
			want:     []string{"README.md", "q1.md", "q?.md", "star*.md", "starry.md"},
		},
		{
			name:     "* matching dotfiles",
			patterns: []string{"*"},
			want:     []string{".hidden", "README.md", "build", "q1.md", "q?.md", "star*.md", "starry.md"},
		},
		{
			name:     "dotfiles at any depth",
			patterns: []string{"**/.*"},
			want:     []string{".hidden", "docs/.draft.md"},
		},
		{
			name:     "?",
			patterns: []string{"workshop/content/?.md"},
			want: []string{
				"workshop/content/!.md",
				"workshop/content/a.md",
				"workshop/content/b.md",
				"workshop/content/c.md",
			},
		},
		{
			name:     "a range",
			patterns: []string{"workshop/content/[a-b].md"},
			want:     []string{"workshop/content/a.md", "workshop/content/b.md"},
		},
		{
			name:     "[^x]",
			patterns: []string{"workshop/content/[^a].md"},
			want:     []string{"workshop/content/!.md", "workshop/content/b.md", "workshop/content/c.md"},
		},
		{
			name:     "[!x]",
			patterns: []string{"workshop/content/[!a].md"},
			want:     []string{"workshop/content/!.md", "workshop/content/b.md", "workshop/content/c.md"},
		},
		{
			name:     "an escaped ! in a class",
			patterns: []string{`workshop/content/[\!a].md`},
			want:     []string{"workshop/content/!.md", "workshop/content/a.md"},
		},
		{
			name:     "{a,b}",
			patterns: []string{"workshop/content/{a,index}.md"},
			want:     []string{"workshop/content/a.md", "workshop/content/index.md"},
		},
		{
			name:     "nested {a,b}",
			patterns: []string{"workshop/content/{a,{b,c}}.md"},
			want:     []string{"workshop/content/a.md", "workshop/content/b.md", "workshop/content/c.md"},
		},
		{
			name:     "{a,b} holding directories",
			patterns: []string{"{docs/deep,exercises}/**"},
			want:     []string{"docs/deep/deeper/most.md", "docs/deep/more.md", "exercises/run.sh"},
		},
		{
			name:     "{a,b} with an empty alternative",
			patterns: []string{"README{.md,}"},
			want:     []string{"README.md"},
		},
		{
			name:     "an unescaped *",
			patterns: []string{"star*.md"},
			want:     []string{"star*.md", "starry.md"},
		},
		{
			name:     "an escaped *",
			patterns: []string{`star\*.md`},
			want:     []string{"star*.md"},
		},
		{
			name:     "an unescaped ?",
			patterns: []string{"q?.md"},
			want:     []string{"q1.md", "q?.md"},
		},
		{
			name:     "an escaped ?",
			patterns: []string{`q\?.md`},
			want:     []string{"q?.md"},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			entries := "- directory:\n    path: content\n  includePaths:\n"

			for _, pattern := range test.patterns {
				entries += "  - '" + pattern + "'\n"
			}

			want := append([]string{}, test.want...)

			sort.Strings(want)

			assertFiles(t, publishFilesAndList(t, entries, files), want)
		})
	}
}

// A malformed pattern fails the publish, naming the entry, the field and the
// pattern.
func TestPublishFiles_MalformedPatternRefused(t *testing.T) {
	for _, field := range []string{"includePaths", "excludePaths", "legalPaths"} {
		for _, pattern := range []string{"[abc", "{a,b"} {
			t.Run(field+" "+pattern, func(t *testing.T) {
				dir := writeWorkshop(t, publishFiles(`
- directory:
    path: .
  `+field+`:
  - '`+pattern+`'
`), map[string]os.FileMode{
					"workshop/content/index.md": 0o644,
				})

				assertErrorHolds(t, publishRefused(t, dir), "spec.publish.files[0]", field, pattern)
			})
		}
	}
}

// An entry whose path leaves the image root fails the publish, naming the
// entry.
func TestPublishFiles_PathOutsideImageRefused(t *testing.T) {
	for _, target := range []string{"..", "../elsewhere", "workshop/../../elsewhere", "/../elsewhere"} {
		t.Run(target, func(t *testing.T) {
			dir := writeWorkshop(t, publishFiles(`
- directory:
    path: .
- path: `+target+`
  directory:
    path: workshop
`), map[string]os.FileMode{
				"workshop/content/index.md": 0o644,
			})

			assertErrorHolds(t, publishRefused(t, dir), "spec.publish.files[1]", target, "outside the image")
		})
	}
}
