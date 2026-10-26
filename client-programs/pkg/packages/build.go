package packages

import (
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/oci"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/pkg/errors"

	regv1 "github.com/google/go-containerregistry/pkg/v1"
)

const (
	// MarkerKey says an image was built to the package image contract. Its
	// value is the contract version.
	MarkerKey = "dev.educates.extension-package"

	// MarkerValue is the contract version this build produces.
	MarkerValue = "1"

	titleKey   = "org.opencontainers.image.title"
	versionKey = "org.opencontainers.image.version"
	createdKey = "org.opencontainers.image.created"
)

// Child is one platform image of a package image.
type Child struct {
	Platform Platform
	Image    regv1.Image
}

// Build is the assembled package image: one child per platform and the index
// which refers to them.
type Build struct {
	Manifest *Manifest
	Children []Child
	Index    regv1.ImageIndex
	Ignored  []string

	layers []*oci.Layer
}

// Close releases the temporary files holding the children's layers. The
// children and the index cannot be read or pushed once the build is closed.
func (b *Build) Close() error {
	var first error

	for _, layer := range b.layers {
		if err := layer.Close(); err != nil && first == nil {
			first = err
		}
	}

	b.layers = nil

	return first
}

// Metadata is the four keys every child and the index carry.
func Metadata(manifest *Manifest, timestamp time.Time) map[string]string {
	return map[string]string{
		MarkerKey:  MarkerValue,
		titleKey:   manifest.Name,
		versionKey: manifest.Version,
		createdKey: timestamp.UTC().Format(time.RFC3339),
	}
}

// BuildPackageImage assembles the children and the index from a package
// source. Nothing is contacted: each child's layer is held in a temporary
// file until the build is closed.
func BuildPackageImage(sourceDir string, platforms []Platform, timestamp time.Time) (*Build, error) {
	manifest, manifestData, err := LoadManifest(sourceDir)

	if err != nil {
		return nil, err
	}

	ignored, err := IgnoredRootEntries(sourceDir)

	if err != nil {
		return nil, err
	}

	metadata := Metadata(manifest, timestamp)

	build := &Build{Manifest: manifest, Ignored: ignored}

	index := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)

	var addenda []mutate.IndexAddendum

	built := false

	defer func() {
		if !built {
			build.Close()
		}
	}()

	for _, platform := range platforms {
		layer, err := BuildLayer(sourceDir, platform, manifestData, timestamp)

		if err != nil {
			return nil, err
		}

		build.layers = append(build.layers, layer)

		image, err := mutate.AppendLayers(mutate.MediaType(empty.Image, types.OCIManifestSchema1), layer)

		if err != nil {
			return nil, errors.Wrapf(err, "unable to assemble the image for %s", platform)
		}

		configFile, err := image.ConfigFile()

		if err != nil {
			return nil, errors.Wrapf(err, "unable to read the config for %s", platform)
		}

		configFile = configFile.DeepCopy()
		configFile.OS = platform.OS
		configFile.Architecture = platform.Architecture
		configFile.Created = regv1.Time{Time: timestamp.UTC()}

		if configFile.Config.Labels == nil {
			configFile.Config.Labels = map[string]string{}
		}

		for key, value := range metadata {
			configFile.Config.Labels[key] = value
		}

		image, err = mutate.ConfigFile(image, configFile)

		if err != nil {
			return nil, errors.Wrapf(err, "unable to set the config for %s", platform)
		}

		// go-containerregistry does not infer the platform of a child, so the
		// descriptor carries it explicitly.
		addenda = append(addenda, mutate.IndexAddendum{
			Add: image,
			Descriptor: regv1.Descriptor{
				Platform: &regv1.Platform{OS: platform.OS, Architecture: platform.Architecture},
			},
		})

		build.Children = append(build.Children, Child{Platform: platform, Image: image})
	}

	index = mutate.AppendManifests(index, addenda...)
	index = mutate.Annotations(index, metadata).(regv1.ImageIndex)

	build.Index = index

	built = true

	return build, nil
}
