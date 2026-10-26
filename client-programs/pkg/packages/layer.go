package packages

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/oci"
	"github.com/pkg/errors"
)

// CommonDirName holds the files every platform child shares.
const CommonDirName = "common"

// Platform is one of the platforms a package image is built for. The contract
// reserves exactly these two.
type Platform struct {
	OS           string
	Architecture string
}

// SupportedPlatforms are the only platforms a package image may be built for.
var SupportedPlatforms = []Platform{
	{OS: "linux", Architecture: "amd64"},
	{OS: "linux", Architecture: "arm64"},
}

func (p Platform) String() string {
	return p.OS + "/" + p.Architecture
}

// DirName is the reserved source directory holding this platform's files.
func (p Platform) DirName() string {
	return p.OS + "-" + p.Architecture
}

// ParsePlatform accepts only the reserved platform names.
func ParsePlatform(value string) (Platform, error) {
	for _, platform := range SupportedPlatforms {
		if platform.String() == value {
			return platform, nil
		}
	}

	names := make([]string, 0, len(SupportedPlatforms))

	for _, platform := range SupportedPlatforms {
		names = append(names, platform.String())
	}

	return Platform{}, errors.Errorf("platform %q is not supported, expected one of %s", value, strings.Join(names, ", "))
}

// defaultTimestamp is the fixed modification time used for every entry when
// SOURCE_DATE_EPOCH is not set.
var defaultTimestamp = oci.DefaultTimestamp

// Timestamp returns the fixed time to stamp on every entry and on the image,
// honouring the reproducible builds convention when SOURCE_DATE_EPOCH is set.
func Timestamp(environ func(string) string) (time.Time, error) {
	return oci.Timestamp(environ)
}

// collectEntries overlays the common directory with a platform directory,
// platform winning on conflict. Every directory walked gets an entry, empty
// ones included, and every other item becomes a file entry for the layer
// builder to publish or refuse.
func collectEntries(sourceDir string, platform Platform) ([]oci.LayerEntry, error) {
	entries := map[string]oci.LayerEntry{}

	for _, dirName := range []string{CommonDirName, platform.DirName()} {
		root := filepath.Join(sourceDir, dirName)

		info, err := os.Stat(root)

		if err != nil {
			if os.IsNotExist(err) {
				// An arch-independent package has no platform directory, and a
				// package whose files are all per-platform has no common one.
				continue
			}

			return nil, errors.Wrapf(err, "unable to read %s", root)
		}

		if !info.IsDir() {
			return nil, errors.Errorf("%s in the package source is not a directory", dirName)
		}

		walkErr := filepath.Walk(root, func(sourcePath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if sourcePath == root {
				return nil
			}

			relative, err := filepath.Rel(root, sourcePath)

			if err != nil {
				return err
			}

			imagePath := filepath.ToSlash(relative)

			// The layer builder skips these too, but pruning them here keeps a
			// source holding nothing else from passing as having files.
			if oci.IsSkippedName(info.Name()) {
				if info.IsDir() {
					return filepath.SkipDir
				}

				return nil
			}

			if info.IsDir() {
				entries[imagePath] = oci.LayerEntry{Path: imagePath, Directory: true}
			} else {
				entries[imagePath] = oci.LayerEntry{Path: imagePath, Source: sourcePath}
			}

			return nil
		})

		if walkErr != nil {
			return nil, errors.Wrapf(walkErr, "unable to read the %s directory of the package source", dirName)
		}
	}

	ordered := make([]oci.LayerEntry, 0, len(entries))

	for _, entry := range entries {
		ordered = append(ordered, entry)
	}

	return ordered, nil
}

// BuildLayer writes the layer for one platform child: the common directory
// overlaid with the platform directory, then the manifest at the image root.
// The caller closes the layer once it has been pushed.
func BuildLayer(sourceDir string, platform Platform, manifest []byte, timestamp time.Time) (*oci.Layer, error) {
	entries, err := collectEntries(sourceDir, platform)

	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, errors.Errorf("the package source has no files for %s, only the manifest would be published", platform)
	}

	// The manifest is copied to the image root under both deliveries, after
	// everything else.
	layer, err := oci.BuildLayer(entries, timestamp, oci.AppendFile(ManifestFileName, manifest))

	if err != nil {
		return nil, errors.Wrapf(err, "unable to build the layer for %s", platform)
	}

	return layer, nil
}

// IgnoredRootEntries lists what was found beside the reserved directories and
// left out of the image, so a publish can say what it did not ship.
func IgnoredRootEntries(sourceDir string) ([]string, error) {
	items, err := os.ReadDir(sourceDir)

	if err != nil {
		return nil, errors.Wrapf(err, "unable to read %s", sourceDir)
	}

	reserved := map[string]bool{CommonDirName: true, ManifestFileName: true}

	for _, platform := range SupportedPlatforms {
		reserved[platform.DirName()] = true
	}

	var ignored []string

	for _, item := range items {
		if reserved[item.Name()] || oci.IsSkippedName(item.Name()) {
			continue
		}

		ignored = append(ignored, item.Name())
	}

	sort.Strings(ignored)

	return ignored, nil
}
