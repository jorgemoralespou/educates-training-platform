package cmd

import (
	"path"
	"path/filepath"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// fetchPackageEntry is one package image for the package fetcher to place on
// disk. It mirrors the entry the session manager writes on the cluster path,
// so the fetcher reads one configuration shape whoever wrote it.
type fetchPackageEntry struct {
	Path  string `json:"path"`
	Image string `json:"image"`
}

// splitPackages is how each extension package declared as an image reaches
// the session. A package appears in exactly one of these, because the base
// image derives which packages were mounted from the absence of a fetcher
// entry.
type splitPackages struct {
	// Mounts are compose volumes, one per package the daemon will mount.
	Mounts []composetypes.ServiceVolumeConfig

	// Fetches are the entries written to the fetcher's configuration.
	Fetches []fetchPackageEntry

	// Names are the packages declared as an image, in declaration order,
	// whichever way they are delivered, so the deploy can report on each.
	Names []string
}

// splitImagePackages sorts the extension packages declared as an image into
// the delivery chosen for this deploy. Packages declaring files are left
// alone: vendir downloads those, exactly as before.
func splitImagePackages(
	workshop *unstructured.Unstructured,
	mounting bool,
	localRepository string,
	name string,
	version string,
) (splitPackages, error) {
	packages := splitPackages{}

	workshopVersion, found, _ := unstructured.NestedString(workshop.Object, "spec", "version")

	if !found {
		workshopVersion = version
	}

	packagesItems, found, _ := unstructured.NestedSlice(workshop.Object, "spec", "workshop", "packages")

	if !found || len(packagesItems) == 0 {
		return packages, nil
	}

	for _, packagesItem := range packagesItems {
		item, ok := packagesItem.(map[string]interface{})

		if !ok {
			return packages, errors.New("unable to parse extension package, entry is not an object")
		}

		tmpName, found := item["name"]

		if !found {
			continue
		}

		packageName, ok := tmpName.(string)

		if !ok {
			return packages, errors.Errorf("unable to parse extension package, name %v is not a string", tmpName)
		}

		tmpImage, found := item["image"]

		if !found || tmpImage == nil {
			continue
		}

		packageImage, ok := tmpImage.(string)

		if !ok {
			return packages, errors.Errorf(
				"unable to parse extension package %q, image is not a string", packageName,
			)
		}

		// The two are mutually exclusive, which the cluster enforces in the
		// CRD. The docker renderer reads the definition without a schema, so
		// it says so itself rather than silently honouring one of them.
		if files, found := item["files"]; found && files != nil {
			return packages, errors.Errorf(
				"extension package %q declares both image and files, which are mutually exclusive", packageName,
			)
		}

		packagePath := filepath.Clean(path.Join("/opt/packages", packageName))

		reference := expandPackageImageTokens(packageImage, localRepository, name, workshopVersion)

		packages.Names = append(packages.Names, packageName)

		if mounting {
			packages.Mounts = append(packages.Mounts, composetypes.ServiceVolumeConfig{
				Type:     "image",
				Source:   reference,
				Target:   packagePath,
				ReadOnly: true,
			})

			continue
		}

		packages.Fetches = append(packages.Fetches, fetchPackageEntry{
			Path:  packagePath,
			Image: reference,
		})
	}

	return packages, nil
}

// expandPackageImageTokens substitutes the three tokens a package image
// reference may use, the same three the vendir path substitutes.
func expandPackageImageTokens(reference string, localRepository string, name string, version string) string {
	reference = strings.ReplaceAll(reference, "$(image_repository)", localRepository)
	reference = strings.ReplaceAll(reference, "$(workshop_name)", name)
	reference = strings.ReplaceAll(reference, "$(workshop_version)", version)

	return reference
}

// generateFetchPackagesConfig renders the fetcher's configuration. With no
// entries it returns an empty string, because the base image treats the
// file's presence as the signal that there is something to fetch.
func generateFetchPackagesConfig(entries []fetchPackageEntry) (string, error) {
	if len(entries) == 0 {
		return "", nil
	}

	config := map[string]interface{}{
		"apiVersion": "packages.educates.dev/v1alpha1",
		"kind":       "PackageFetch",
		"packages":   entries,
	}

	configBytes, err := yaml.Marshal(&config)

	if err != nil {
		return "", errors.Wrap(err, "failed to generate package fetch config")
	}

	return string(configBytes), nil
}
