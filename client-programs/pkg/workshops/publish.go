package workshops

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	yttcmdui "carvel.dev/ytt/pkg/cmd/ui"
	"carvel.dev/ytt/pkg/files"
	"carvel.dev/ytt/pkg/yamlmeta"
	"github.com/educates/educates-training-platform/client-programs/pkg/oci"
	regname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/kubectl/pkg/scheme"

	regv1 "github.com/google/go-containerregistry/pkg/v1"
)

type FilesPublishOptions struct {
	Image           string
	Repository      string
	WorkshopFile    string
	ExportWorkshop  string
	WorkshopVersion string
	Registry        oci.RegistryOptions
	DataValuesFlags yttcmd.DataValuesFlags
}

func (o *FilesPublishOptions) Run(stdout io.Writer, args []string) error {
	var err error

	var workshopDir string

	if len(args) != 0 {
		workshopDir = filepath.Clean(args[0])
	} else {
		workshopDir = "."
	}

	if workshopDir, err = filepath.Abs(workshopDir); err != nil {
		return errors.Wrap(err, "couldn't convert workshop directory to absolute path")
	}

	fileInfo, err := os.Stat(workshopDir)

	if err != nil || !fileInfo.IsDir() {
		return errors.New("workshop directory does not exist or path is not a workshop directory")
	}

	return o.Publish(stdout, workshopDir)
}

func (o *FilesPublishOptions) Publish(stdout io.Writer, workshopDir string) error {
	// If image name hasn't been supplied read workshop definition file and
	// try to work out image name to publish workshop as.

	rootDirectory := workshopDir
	workshopFilePath := o.WorkshopFile

	workingDirectory, err := os.Getwd()

	if err != nil {
		return errors.Wrap(err, "cannot determine current working directory")
	}

	if !filepath.IsAbs(workshopFilePath) {
		workshopFilePath = filepath.Join(rootDirectory, workshopFilePath)
	}

	workshopFileData, err := os.ReadFile(workshopFilePath)

	if err != nil {
		return errors.Wrapf(err, "cannot open workshop definition %q", workshopFilePath)
	}

	// Process the workshop YAML data for ytt templating and data variables.

	if workshopFileData, err = ProcessWorkshopDefinition(workshopFileData, o.DataValuesFlags); err != nil {
		return errors.Wrap(err, "unable to process workshop definition as template")
	}

	workshopFileData = []byte(strings.ReplaceAll(string(workshopFileData), "$(image_repository)", o.Repository))
	workshopFileData = []byte(strings.ReplaceAll(string(workshopFileData), "$(workshop_version)", o.WorkshopVersion))

	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDecoder()

	workshop := &unstructured.Unstructured{}

	err = runtime.DecodeInto(decoder, workshopFileData, workshop)

	if err != nil {
		return errors.Wrap(err, "couldn't parse workshop definition")
	}

	fmt.Fprintf(stdout, "Processing workshop with name %q.\n", workshop.GetName())

	if workshop.GetAPIVersion() != "training.educates.dev/v1beta1" || workshop.GetKind() != "Workshop" {
		return errors.New("invalid type for workshop definition")
	}

	image := o.Image

	if image == "" {
		image, _, _ = unstructured.NestedString(workshop.Object, "spec", "publish", "image")
	}

	if image == "" {
		return errors.Errorf("cannot find image name for publishing workshop %q", workshopFilePath)
	}

	reference, err := o.Registry.ParseTag(image)

	if err != nil {
		return err
	}

	// The content published is the workshop directory, unless publish.files
	// names what to assemble in its place.

	var entries []oci.LayerEntry

	if fileArtifacts, found, _ := unstructured.NestedSlice(workshop.Object, "spec", "publish", "files"); found && len(fileArtifacts) != 0 {
		entries, err = filesEntries(workshopDir, fileArtifacts)
	} else {
		entries, err = contentEntries(workshopDir)
	}

	if err != nil {
		return err
	}

	// Now publish the content as an OCI image.

	fmt.Fprintf(stdout, "Publishing workshop files to %q.\n", image)

	digest, err := o.push(reference, entries)

	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Pushed '%s@%s'\n", reference.Context().Name(), digest)

	// We add a newline to output for better readability.
	fmt.Fprintln(stdout)

	// Export modified workshop definition file.

	exportWorkshop := o.ExportWorkshop

	if exportWorkshop != "" {
		// Insert workshop version property if not specified.

		_, found, _ := unstructured.NestedString(workshop.Object, "spec", "version")

		if !found && o.WorkshopVersion != "latest" {
			unstructured.SetNestedField(workshop.Object, o.WorkshopVersion, "spec", "version")
		}

		// Remove the publish section as will not be accurate after publishing.

		unstructured.RemoveNestedField(workshop.Object, "spec", "publish")

		workshopFileData, err = yaml.Marshal(&workshop.Object)

		if err != nil {
			return errors.Wrap(err, "couldn't convert workshop definition back to YAML")
		}

		if !filepath.IsAbs(exportWorkshop) {
			exportWorkshop = filepath.Join(workingDirectory, exportWorkshop)
		}

		exportWorkshopFile, err := os.Create(exportWorkshop)

		if err != nil {
			return errors.Wrap(err, "unable to create exported workshop definition file")
		}

		defer exportWorkshopFile.Close()

		_, err = exportWorkshopFile.Write(workshopFileData)

		if err != nil {
			return errors.Wrap(err, "unable to write exported workshop definition file")
		}
	}

	return nil
}

// push builds the workshop image from the layer entries and pushes it under
// the reference, returning the digest it was pushed as. The image is a single
// OCI image manifest whose one layer holds the content at its root.
func (o *FilesPublishOptions) push(reference regname.Tag, entries []oci.LayerEntry) (regv1.Hash, error) {
	timestamp, err := oci.Timestamp(os.Getenv)

	if err != nil {
		return regv1.Hash{}, err
	}

	layer, err := oci.BuildLayer(entries, timestamp)

	if err != nil {
		return regv1.Hash{}, errors.Wrap(err, "unable to build the workshop image")
	}

	defer layer.Close()

	base := mutate.ConfigMediaType(mutate.MediaType(empty.Image, types.OCIManifestSchema1), types.OCIConfigJSON)

	image, err := mutate.AppendLayers(base, layer)

	if err != nil {
		return regv1.Hash{}, errors.Wrap(err, "unable to assemble the workshop image")
	}

	remoteOptions, err := o.Registry.RemoteOptions()

	if err != nil {
		return regv1.Hash{}, err
	}

	if err := remote.Write(reference, image, remoteOptions...); err != nil {
		return regv1.Hash{}, errors.Wrap(err, "unable to push image artifact for workshop")
	}

	digest, err := image.Digest()

	if err != nil {
		return regv1.Hash{}, errors.Wrap(err, "unable to compute the workshop image digest")
	}

	return digest, nil
}

// contentEntries lists the files under the content directory, and the
// directories holding them, as entries for the layer. A .git at the root of
// the content is left out, as are the macOS artefacts a layer never holds.
// Anything which is neither a file nor a directory is passed on as a file
// entry, for the layer builder to refuse.
func contentEntries(contentDirectory string) ([]oci.LayerEntry, error) {
	// The directory itself may be named through a symbolic link, which the
	// walk would not follow.
	contentDirectory, err := filepath.EvalSymlinks(contentDirectory)

	if err != nil {
		return nil, errors.Wrap(err, "unable to read the workshop files")
	}

	// Each file keeps its path from the content directory in the image, so
	// the .git at the root of the image is left out of the walk rather than
	// walked through.
	files, err := sourceFiles(contentDirectory, isImageGit)

	if err != nil {
		return nil, errors.Wrap(err, "unable to read the workshop files")
	}

	placed := make(map[string]string, len(files))

	for _, file := range files {
		placed[file] = filepath.Join(contentDirectory, filepath.FromSlash(file))
	}

	return toLayerEntries(placed), nil
}

/*
 * ProcessWorkshopDefinition processes a workshop YAML definition file through the ytt templating engine.
 * It takes the raw YAML data as input along with any data value flags for template variable substitution.
 * The function returns the processed YAML with template variables replaced, or an error if processing fails.
 */

func ProcessWorkshopDefinition(yamlData []byte, dataValueFlags yttcmd.DataValuesFlags) ([]byte, error) {
	templatingOptions := yttcmd.NewOptions()

	templatingOptions.IgnoreUnknownComments = true

	templatingOptions.DataValuesFlags = dataValueFlags

	var filesToProcess []*files.File

	mainInputFile := files.MustNewFileFromSource(files.NewBytesSource("workshop.yaml", yamlData))

	filesToProcess = append(filesToProcess, mainInputFile)

	logUI := yttcmdui.NewCustomWriterTTY(false, log.Writer(), log.Writer())

	output := templatingOptions.RunWithFiles(yttcmd.Input{Files: filesToProcess}, logUI)

	if output.Err != nil {
		return []byte{}, fmt.Errorf("execution of ytt failed: %s", output.Err)
	}

	if len(output.DocSet.Items) == 0 {
		return []byte{}, nil
	}

	var buf bytes.Buffer

	yamlmeta.NewYAMLPrinter(&buf).Print(output.DocSet.Items[0])

	return buf.Bytes(), nil
}
