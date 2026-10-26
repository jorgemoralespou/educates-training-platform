package workshops

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/educates/educates-training-platform/client-programs/pkg/oci"
	"github.com/pkg/errors"
)

// remoteSources are the source types a publish.files entry could once name
// besides directory, in the order an entry naming several is reported by.
var remoteSources = []string{
	"git",
	"hg",
	"http",
	"image",
	"imgpkgBundle",
	"githubRelease",
	"helmChart",
	"manual",
	"inline",
}

// filesEntry holds the fields of a publish.files entry which publishing
// reads. Any other field is ignored.
type filesEntry struct {
	Path      string `json:"path"`
	Directory *struct {
		Path string `json:"path"`
	} `json:"directory"`
	IncludePaths []string  `json:"includePaths"`
	ExcludePaths []string  `json:"excludePaths"`
	LegalPaths   *[]string `json:"legalPaths"`
	NewRootPath  string    `json:"newRootPath"`

	// fieldPath names the entry in the workshop definition, as
	// spec.publish.files[0], for errors to say which entry they are about.
	fieldPath string
}

// defaultLegalPaths keep the licence, copyright and notice files at the
// content root when an entry gives no legalPaths.
var defaultLegalPaths = []string{
	"{LICENSE,LICENCE,License,Licence}{,.md,.txt,.rst}",
	"{COPYRIGHT,Copyright}{,.md,.txt,.rst}",
	"{NOTICE,Notice}{,.md,.txt,.rst}",
}

// filesLayerEntries resolves the publish.files entries into the layer
// entries, taking each file from its source in place. The publish.files
// entries apply in order, each first removing what earlier ones placed at or
// under its path. A .git at the root of the result is left out.
func filesLayerEntries(workshopDir string, fileArtifacts []interface{}) ([]oci.LayerEntry, error) {
	// The files placed so far, by their path in the image, each naming its
	// source on disk.
	assembled := map[string]string{}

	for index, artifactEntry := range fileArtifacts {
		entry, err := readFilesEntry(fmt.Sprintf("spec.publish.files[%d]", index), artifactEntry)

		if err != nil {
			return nil, err
		}

		target, err := entry.target()

		if err != nil {
			return nil, err
		}

		sourceDirectory, err := entry.sourceDirectory(workshopDir)

		if err != nil {
			return nil, err
		}

		filter, err := entry.filter()

		if err != nil {
			return nil, err
		}

		newRoot, err := entry.newRoot(sourceDirectory)

		if err != nil {
			return nil, err
		}

		files, err := sourceFiles(sourceDirectory, nil)

		if err != nil {
			return nil, errors.Wrapf(err, "unable to read the files of %s", entry.fieldPath)
		}

		files = underNewRoot(filter.keep(files), newRoot)

		if len(files) == 0 {
			return nil, errors.Errorf("%s has no files to publish", entry.fieldPath)
		}

		contentRoot := filepath.Join(sourceDirectory, filepath.FromSlash(newRoot))

		for imagePath := range assembled {
			if isAtOrUnder(imagePath, target) {
				delete(assembled, imagePath)
			}
		}

		for parent := path.Dir(target); parent != "."; parent = path.Dir(parent) {
			if _, found := assembled[parent]; found {
				return nil, errors.Errorf("%s has the path %q, which lies under the file %q an earlier entry placed", entry.fieldPath, entry.Path, parent)
			}
		}

		for _, file := range files {
			assembled[path.Join(target, file)] = filepath.Join(contentRoot, filepath.FromSlash(file))
		}
	}

	return toLayerEntries(assembled), nil
}

// readFilesEntry reads the publish.files entry at the field path, refusing
// any which does not name a directory to take its files from.
func readFilesEntry(fieldPath string, artifactEntry interface{}) (*filesEntry, error) {
	fields, ok := artifactEntry.(map[string]interface{})

	if !ok {
		return nil, errors.Errorf("%s is not a mapping", fieldPath)
	}

	for _, source := range remoteSources {
		if fields[source] != nil {
			return nil, errors.Errorf("%s uses the %q source, but remote sources were removed from publish.files in 4.0.0: "+
				"place the files in the workshop directory and name them with a directory entry", fieldPath, source)
		}
	}

	data, err := json.Marshal(fields)

	if err != nil {
		return nil, errors.Wrapf(err, "unable to read %s", fieldPath)
	}

	entry := &filesEntry{fieldPath: fieldPath}

	if err := json.Unmarshal(data, entry); err != nil {
		return nil, errors.Wrapf(err, "unable to read %s", fieldPath)
	}

	if entry.Directory == nil || entry.Directory.Path == "" {
		return nil, errors.Errorf("%s names no directory.path to take its files from", fieldPath)
	}

	return entry, nil
}

// target cleans the path the entry places its files under, relative to the
// image root, which is returned as empty. A path leaving the image root is
// refused.
func (e *filesEntry) target() (string, error) {
	cleaned, inside := oci.CleanRelativePath(e.Path)

	if !inside {
		return "", errors.Errorf("%s has the path %q, which is outside the image", e.fieldPath, e.Path)
	}

	return cleaned, nil
}

// sourceDirectory finds the directory the entry takes its files from. A
// relative path is taken from the workshop directory, and the directory may
// be named through a symbolic link.
func (e *filesEntry) sourceDirectory(workshopDir string) (string, error) {
	directory := filepath.Clean(e.Directory.Path)

	if !filepath.IsAbs(directory) {
		directory = filepath.Join(workshopDir, directory)
	}

	resolved, err := filepath.EvalSymlinks(directory)

	if err != nil {
		return "", errors.Wrapf(err, "unable to find the directory.path of %s", e.fieldPath)
	}

	info, err := os.Stat(resolved)

	if err != nil {
		return "", errors.Wrapf(err, "unable to find the directory.path of %s", e.fieldPath)
	}

	if !info.IsDir() {
		return "", errors.Errorf("the directory.path of %s, %s, is not a directory", e.fieldPath, directory)
	}

	return resolved, nil
}

// newRoot cleans the newRootPath of the entry, relative to the root of its
// content, which is returned as empty. It must name a directory in the
// content.
func (e *filesEntry) newRoot(sourceDirectory string) (string, error) {
	cleaned, inside := oci.CleanRelativePath(e.NewRootPath)

	if !inside {
		return "", errors.Errorf("%s has the newRootPath %q, which is outside the content", e.fieldPath, e.NewRootPath)
	}

	if cleaned == "" {
		return "", nil
	}

	info, err := os.Lstat(filepath.Join(sourceDirectory, filepath.FromSlash(cleaned)))

	if os.IsNotExist(err) {
		return "", errors.Errorf("%s has the newRootPath %q, which does not exist", e.fieldPath, e.NewRootPath)
	}

	if err != nil {
		return "", errors.Wrapf(err, "unable to find the newRootPath of %s", e.fieldPath)
	}

	if !info.IsDir() {
		return "", errors.Errorf("%s has the newRootPath %q, which is not a directory", e.fieldPath, e.NewRootPath)
	}

	return cleaned, nil
}

// underNewRoot keeps the files under the new root, by their path from it.
func underNewRoot(files []string, newRoot string) []string {
	if newRoot == "" {
		return files
	}

	var rooted []string

	for _, file := range files {
		if relative, found := strings.CutPrefix(file, newRoot+"/"); found {
			rooted = append(rooted, relative)
		}
	}

	return rooted
}

// fileFilter selects the files an entry keeps, by the patterns it gives.
type fileFilter struct {
	includePaths []string
	excludePaths []string
	legalPaths   []string
}

// filter builds the file filter from the patterns the entry gives.
func (e *filesEntry) filter() (*fileFilter, error) {
	filter := &fileFilter{}

	var err error

	if filter.includePaths, err = e.patterns("includePaths", e.IncludePaths); err != nil {
		return nil, err
	}

	if filter.excludePaths, err = e.patterns("excludePaths", e.ExcludePaths); err != nil {
		return nil, err
	}

	legalPaths := defaultLegalPaths

	if e.LegalPaths != nil {
		legalPaths = *e.LegalPaths
	}

	if filter.legalPaths, err = e.patterns("legalPaths", legalPaths); err != nil {
		return nil, err
	}

	return filter, nil
}

// patterns puts the patterns the entry gives in a field into the form files
// are matched against, a path from the content root without a leading or
// trailing slash, refusing any pattern which is malformed.
func (e *filesEntry) patterns(field string, patterns []string) ([]string, error) {
	cleaned := make([]string, 0, len(patterns))

	for _, pattern := range patterns {
		if !doublestar.ValidatePattern(pattern) {
			return nil, errors.Errorf("%s has the %s pattern %q, which is not a valid pattern", e.fieldPath, field, pattern)
		}

		cleaned = append(cleaned, path.Clean(strings.TrimLeft(pattern, "/")))
	}

	return cleaned, nil
}

// keep returns the files the filter selects: those matching an includePaths
// pattern, or all when there are none, less those matching an excludePaths
// pattern. A file matching a legalPaths pattern is kept regardless.
func (f *fileFilter) keep(files []string) []string {
	var kept []string

	for _, file := range files {
		included := len(f.includePaths) == 0 || matchesAny(f.includePaths, file)

		if included && !matchesAny(f.excludePaths, file) || matchesAny(f.legalPaths, file) {
			kept = append(kept, file)
		}
	}

	return kept
}

func matchesAny(patterns []string, file string) bool {
	for _, pattern := range patterns {
		if doublestar.MatchUnvalidated(pattern, file) {
			return true
		}
	}

	return false
}

// sourceFiles lists everything under a directory which is not a directory,
// by its slash-separated path relative to it, leaving out the macOS
// artefacts a layer never holds and whatever leaveOut, when given, reports
// by that path. A directory left out is not walked through. Symbolic links
// are listed as they are, for the layer builder to refuse.
func sourceFiles(directory string, leaveOut func(relative string) bool) ([]string, error) {
	var files []string

	err := filepath.WalkDir(directory, func(sourcePath string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if sourcePath == directory {
			return nil
		}

		relative, err := filepath.Rel(directory, sourcePath)

		if err != nil {
			return err
		}

		relative = filepath.ToSlash(relative)

		if oci.IsMacOSMetadataFile(item.Name()) || leaveOut != nil && leaveOut(relative) {
			if item.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if item.IsDir() {
			return nil
		}

		files = append(files, relative)

		return nil
	})

	return files, err
}

// isImageGit reports the .git at the root of the image, and anything under
// it, which is never published.
func isImageGit(imagePath string) bool {
	return isAtOrUnder(imagePath, ".git")
}

// isAtOrUnder reports whether a slash-separated path is the given directory
// or lies under it. Everything lies under the root, given as empty.
func isAtOrUnder(name string, directory string) bool {
	return directory == "" || name == directory || strings.HasPrefix(name, directory+"/")
}

// toLayerEntries turns the files placed in the image, by their path in it,
// each naming its source on disk, into the entries of the layer, with an
// entry for each directory holding a file. A .git at the root of the image
// is left out.
func toLayerEntries(placed map[string]string) []oci.LayerEntry {
	var layerEntries []oci.LayerEntry

	directories := map[string]bool{}

	for imagePath, source := range placed {
		if isImageGit(imagePath) {
			continue
		}

		layerEntries = append(layerEntries, oci.LayerEntry{Path: imagePath, Source: source})

		layerEntries = appendParents(layerEntries, directories, imagePath)
	}

	return layerEntries
}

// appendParents appends an entry for each directory above a file in the
// layer which has none yet. A directory gets an entry only for holding a
// file, and once its entry is there so are those of the directories above
// it.
func appendParents(layerEntries []oci.LayerEntry, directories map[string]bool, imagePath string) []oci.LayerEntry {
	for parent := path.Dir(imagePath); parent != "." && !directories[parent]; parent = path.Dir(parent) {
		directories[parent] = true

		layerEntries = append(layerEntries, oci.LayerEntry{Path: parent, Directory: true})
	}

	return layerEntries
}
