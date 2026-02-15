package educates

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	imgpkgcmd "carvel.dev/imgpkg/pkg/imgpkg/cmd"
	vendirsync "carvel.dev/vendir/pkg/vendir/cmd"
	yttcmd "carvel.dev/ytt/pkg/cmd/template"

	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/logger"
	"github.com/educates/educates-training-platform/client-programs/pkg/templates"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"go.yaml.in/yaml/v2"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion/scheme"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

type WorkshopDefinitionManager struct {
}

type NewWorkshopDefinitionConfig struct {
	Template              string
	Name                  string
	Title                 string
	Description           string
	Image                 string
	TargetDirectory       string
	Overwrite             bool
	WithKubernetesAccess  bool
	WithGitHubAction      bool
	WithVirtualCluster    bool
	WithDockerDaemon      bool
	WithImageRegistry     bool
	WithKubernetesConsole bool
	WithEditor            bool
	WithTerminal          bool
}

type ExportWorkshopDefinitionConfig struct {
	Repository      string
	WorkshopFile    string
	WorkshopVersion string
	DataValuesFlags yttcmd.DataValuesFlags
}

type PublishWorkshopDefinitionConfig struct {
	Image           string
	Repository      string
	WorkshopFile    string
	ExportWorkshop  string
	WorkshopVersion string
	RegistryFlags   imgpkgcmd.RegistryFlags
	DataValuesFlags yttcmd.DataValuesFlags
}

type NewWorkshopBundleConfig struct {
	Name                  string
	Template              string
	Title                 string
	Description           string
	Image                 string
	Overwrite             bool
	WorkshopNames         []string
	WithGitHubAction      bool
	WithKubernetesAccess  bool
	WithVirtualCluster    bool
	WithDockerDaemon      bool
	WithImageRegistry     bool
	WithKubernetesConsole bool
	WithEditor            bool
	WithTerminal          bool
}

type BundleWorkshop struct {
	Name      string
	Directory string
}

type PublishWorkshopBundleConfig struct {
	PublishWorkshopDefinitionConfig
	Workshops []string
	FailFast  bool
}

type ExportWorkshopBundleConfig struct {
	ExportWorkshopDefinitionConfig
	Workshops []string
}

func NewWorkshopDefinitionManager() *WorkshopDefinitionManager {
	return &WorkshopDefinitionManager{}
}

func (m *WorkshopDefinitionManager) New(directory string, o *NewWorkshopDefinitionConfig) error {
	var err error

	parameters := map[string]string{
		"WorkshopName":          o.Name,
		"WorkshopTitle":         o.Title,
		"WorkshopDescription":   o.Description,
		"WorkshopImage":         o.Image,
		"WithKubernetesAccess":  strconv.FormatBool(o.WithKubernetesAccess),
		"WithVirtualCluster":    strconv.FormatBool(o.WithVirtualCluster),
		"WithDockerDaemon":      strconv.FormatBool(o.WithDockerDaemon),
		"WithImageRegistry":     strconv.FormatBool(o.WithImageRegistry),
		"WithKubernetesConsole": strconv.FormatBool(o.WithKubernetesConsole),
		"WithEditor":            strconv.FormatBool(o.WithEditor),
		"WithTerminal":          strconv.FormatBool(o.WithTerminal),
	}

	template := templates.InternalTemplate(o.Template)

	err = template.ApplyFiles(directory, parameters)

	if err != nil {
		return errors.Wrap(err, "unable to apply template")
	}

	if o.WithGitHubAction {
		template := templates.InternalTemplate("single")
		err = template.ApplyGitHubAction(directory, parameters)
	}

	return err
}

func (m *WorkshopDefinitionManager) Export(directory string, o *ExportWorkshopDefinitionConfig) (string, error) {
	// If image name hasn't been supplied read workshop definition file and
	// try to work out image name to Export workshop as.

	rootDirectory := directory
	workshopFilePath := o.WorkshopFile

	if !filepath.IsAbs(workshopFilePath) {
		workshopFilePath = filepath.Join(rootDirectory, workshopFilePath)
	}

	workshopFileData, err := os.ReadFile(workshopFilePath)

	if err != nil {
		return "", errors.Wrapf(err, "cannot open workshop definition %q", workshopFilePath)
	}

	// Process the workshop YAML data for ytt templating and data variables.

	if workshopFileData, err = ProcessWorkshopDefinition(workshopFileData, o.DataValuesFlags); err != nil {
		return "", errors.Wrap(err, "unable to process workshop definition as template")
	}

	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDecoder()

	workshop := &unstructured.Unstructured{}

	err = runtime.DecodeInto(decoder, workshopFileData, workshop)

	if err != nil {
		return "", errors.Wrap(err, "couldn't parse workshop definition")
	}

	if workshop.GetAPIVersion() != constants.EducatesTrainingAPIGroupVersion || workshop.GetKind() != "Workshop" {
		return "", errors.New("invalid type for workshop definition")
	}

	workshop = utils.SanitizeWorkshopResourceForExport(workshop, &utils.WorkshopResourceExportConfig{
		Repository:      o.Repository,
		WorkshopVersion: o.WorkshopVersion,
	})

	// Export modified workshop definition file.

	workshopFileData, err = yaml.Marshal(&workshop.Object)

	if err != nil {
		return "", errors.Wrap(err, "couldn't convert workshop definition back to YAML")
	}

	return string(workshopFileData), nil
}

func (m *WorkshopDefinitionManager) Publish(directory string, o *PublishWorkshopDefinitionConfig) error {
	// If image name hasn't been supplied read workshop definition file and
	// try to work out image name to publish workshop as.

	rootDirectory := directory
	workshopFilePath := o.WorkshopFile

	workingDirectory, err := os.Getwd()

	if err != nil {
		return errors.Wrap(err, "cannot determine current working directory")
	}

	includePaths := []string{directory}
	excludePaths := []string{".git"}

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

	// Extract vendir snippet describing subset of files to package up as the
	// workshop image.

	carvelUI := logger.NewCarvelUI()

	carvelUI.PrintLinef("Processing workshop with name %q", workshop.GetName())

	if workshop.GetAPIVersion() != constants.EducatesTrainingAPIGroupVersion || workshop.GetKind() != "Workshop" {
		return errors.New("invalid type for workshop definition")
	}

	image := o.Image

	if image == "" {
		image, _, _ = unstructured.NestedString(workshop.Object, "spec", "publish", "image")
	}

	if image == "" {
		return errors.Errorf("cannot find image name for publishing workshop %q", workshopFilePath)
	}

	if fileArtifacts, found, _ := unstructured.NestedSlice(workshop.Object, "spec", "publish", "files"); found && len(fileArtifacts) != 0 {
		tempDir, err := os.MkdirTemp("", "educates-imgpkg")

		if err != nil {
			return errors.Wrapf(err, "unable to create temporary working directory")
		}

		defer os.RemoveAll(tempDir)

		for _, artifactEntry := range fileArtifacts {
			vendirConfig := map[string]interface{}{
				"apiVersion":  "vendir.k14s.io/v1alpha1",
				"kind":        "Config",
				"directories": []interface{}{},
			}

			dir := filepath.Join(tempDir, "files")

			if filePath, found := artifactEntry.(map[string]interface{})["path"].(string); found {
				dir = filepath.Join(tempDir, "files", filepath.Clean(filePath))
			}

			if directoryConfig, found := artifactEntry.(map[string]interface{})["directory"]; found {
				if directoryPath, found := directoryConfig.(map[string]interface{})["path"].(string); found {
					if !filepath.IsAbs(directoryPath) {
						directoryConfig.(map[string]interface{})["path"] = filepath.Join(directory, directoryPath)
					}
				}
			}

			artifactEntry.(map[string]interface{})["path"] = "."

			directoryConfig := map[string]interface{}{
				"path":     dir,
				"contents": []interface{}{artifactEntry},
			}

			vendirConfig["directories"] = append(vendirConfig["directories"].([]interface{}), directoryConfig)

			yamlData, err := yaml.Marshal(&vendirConfig)

			if err != nil {
				return errors.Wrap(err, "unable to generate vendir config")
			}

			vendirConfigFile, err := os.Create(filepath.Join(tempDir, "vendir.yml"))

			if err != nil {
				return errors.Wrap(err, "unable to create vendir config file")
			}

			defer vendirConfigFile.Close()

			_, err = vendirConfigFile.Write(yamlData)

			if err != nil {
				return errors.Wrap(err, "unable to write vendir config file")
			}

			syncOptions := vendirsync.NewSyncOptions(carvelUI)

			syncOptions.Directories = nil
			syncOptions.Files = []string{filepath.Join(tempDir, "vendir.yml")}

			// Note that Chdir here actually changes the process working directory.

			syncOptions.LockFile = filepath.Join(tempDir, "lock-file")
			syncOptions.Locked = false
			syncOptions.Chdir = tempDir
			syncOptions.AllowAllSymlinkDestinations = false

			if err = syncOptions.Run(); err != nil {
				fmt.Println(string(yamlData))

				return errors.Wrap(err, "failed to prepare image files for publishing")
			}
		}

		// Restore working directory as was changed.

		os.Chdir((workingDirectory))

		rootDirectory = filepath.Join(tempDir, "files")
		includePaths = []string{rootDirectory}
	}

	// Now publish workshop directory contents as OCI image artifact.
	carvelUI.PrintLinef("Publishing workshop files to %q", image)

	pushOptions := imgpkgcmd.NewPushOptions(carvelUI)

	pushOptions.ImageFlags.Image = image
	pushOptions.FileFlags.Files = append(pushOptions.FileFlags.Files, includePaths...)
	pushOptions.FileFlags.ExcludedFilePaths = append(pushOptions.FileFlags.ExcludedFilePaths, excludePaths...)

	pushOptions.RegistryFlags = o.RegistryFlags

	err = pushOptions.Run()

	if err != nil {
		return errors.Wrap(err, "unable to push image artifact for workshop")
	}

	// // We add a newline to output for better readability.
	// confUI.PrintLinef("\n")

	// Export modified workshop definition file.
	exportWorkshop := o.ExportWorkshop

	if exportWorkshop != "" {
		workshop = utils.SanitizeWorkshopResourceForExport(workshop, &utils.WorkshopResourceExportConfig{
			Repository:      o.Repository,
			WorkshopVersion: o.WorkshopVersion,
		})

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

func (m *WorkshopDefinitionManager) NewBundle(directory string, o *NewWorkshopBundleConfig) error {
	parameters := map[string]string{
		"BundleName": o.Name,
	}

	template := templates.InternalTemplate("bundle")

	if err := template.ApplyFiles(directory, parameters); err != nil {
		return errors.Wrap(err, "unable to apply bundle template")
	}

	if o.WithGitHubAction {
		githubTemplate := templates.InternalTemplate("bundle")
		if err := githubTemplate.ApplyGitHubAction(directory, parameters); err != nil {
			return errors.Wrap(err, "unable to apply bundle GitHub action template")
		}
	}

	for _, workshopName := range o.WorkshopNames {
		workshopDirectory := filepath.Join(directory, constants.DefaultWorkshopsDirectoryName, workshopName)

		if err := m.New(workshopDirectory, &NewWorkshopDefinitionConfig{
			Template:              o.Template,
			Name:                  workshopName,
			Title:                 o.Title,
			Description:           o.Description,
			Image:                 o.Image,
			Overwrite:             o.Overwrite,
			WithGitHubAction:      false,
			WithKubernetesAccess:  o.WithKubernetesAccess,
			WithVirtualCluster:    o.WithVirtualCluster,
			WithDockerDaemon:      o.WithDockerDaemon,
			WithImageRegistry:     o.WithImageRegistry,
			WithKubernetesConsole: o.WithKubernetesConsole,
			WithEditor:            o.WithEditor,
			WithTerminal:          o.WithTerminal,
		}); err != nil {
			return err
		}
	}

	if len(o.WorkshopNames) != 0 {
		if err := m.updateTrainingPortalWorkshops(directory, o.WorkshopNames); err != nil {
			return err
		}
	}

	return nil
}

func (m *WorkshopDefinitionManager) DiscoverBundleWorkshops(directory string, workshopFile string, requested []string) ([]BundleWorkshop, error) {
	if workshopFile == "" {
		workshopFile = constants.DefaultWorkshopDefinitionPath
	}

	workshopsRoot := filepath.Join(directory, constants.DefaultWorkshopsDirectoryName)

	entries, err := os.ReadDir(workshopsRoot)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read workshops directory %q", workshopsRoot)
	}

	workshopByName := map[string]BundleWorkshop{}
	var workshopNames []string

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		workshopName := entry.Name()
		workshopDirectory := filepath.Join(workshopsRoot, workshopName)
		workshopFilePath := filepath.Join(workshopDirectory, workshopFile)

		if fileInfo, err := os.Stat(workshopFilePath); err == nil && !fileInfo.IsDir() {
			workshopByName[workshopName] = BundleWorkshop{
				Name:      workshopName,
				Directory: workshopDirectory,
			}
			workshopNames = append(workshopNames, workshopName)
		}
	}

	sort.Strings(workshopNames)

	if len(workshopNames) == 0 {
		return nil, errors.Errorf("no workshops found under %q", workshopsRoot)
	}

	if len(requested) == 0 {
		workshops := make([]BundleWorkshop, 0, len(workshopNames))
		for _, workshopName := range workshopNames {
			workshops = append(workshops, workshopByName[workshopName])
		}
		return workshops, nil
	}

	selected := make([]BundleWorkshop, 0, len(requested))

	for _, workshopName := range requested {
		workshop, found := workshopByName[workshopName]
		if !found {
			return nil, errors.Errorf("workshop %q was not found in %q", workshopName, workshopsRoot)
		}
		selected = append(selected, workshop)
	}

	return selected, nil
}

func (m *WorkshopDefinitionManager) PublishBundle(directory string, o *PublishWorkshopBundleConfig) error {
	workshops, err := m.DiscoverBundleWorkshops(directory, o.WorkshopFile, o.Workshops)
	if err != nil {
		return err
	}

	var publishErrors []string

	for _, workshop := range workshops {
		config := o.PublishWorkshopDefinitionConfig
		config.ExportWorkshop = ""

		if o.ExportWorkshop != "" {
			if err := os.MkdirAll(o.ExportWorkshop, os.ModePerm); err != nil {
				return errors.Wrapf(err, "unable to create export directory %q", o.ExportWorkshop)
			}
			config.ExportWorkshop = filepath.Join(o.ExportWorkshop, fmt.Sprintf("%s-workshop.yaml", workshop.Name))
		}

		if err := m.Publish(workshop.Directory, &config); err != nil {
			if o.FailFast {
				return err
			}

			publishErrors = append(publishErrors, fmt.Sprintf("%s: %v", workshop.Name, err))
		} else if len(workshops) > 1 {
			fmt.Println()
		}
	}

	if len(publishErrors) != 0 {
		return errors.Errorf("failed publishing one or more workshops:\n- %s", strings.Join(publishErrors, "\n- "))
	}

	return nil
}

func (m *WorkshopDefinitionManager) ExportBundle(directory string, o *ExportWorkshopBundleConfig) ([]utils.ExportedYAMLDocument, error) {
	workshops, err := m.DiscoverBundleWorkshops(directory, o.WorkshopFile, o.Workshops)
	if err != nil {
		return nil, err
	}

	documents := []utils.ExportedYAMLDocument{}

	trainingPortalData, err := m.exportTrainingPortalWithWorkshopSelection(directory, workshops)
	if err != nil {
		return nil, err
	}

	documents = append(documents, utils.ExportedYAMLDocument{
		Name: "trainingportal.yaml",
		Data: trainingPortalData,
	})

	for _, workshop := range workshops {
		config := o.ExportWorkshopDefinitionConfig

		workshopData, err := m.Export(workshop.Directory, &config)
		if err != nil {
			return nil, err
		}

		documents = append(documents, utils.ExportedYAMLDocument{
			Name: fmt.Sprintf("%s-workshop.yaml", workshop.Name),
			Data: []byte(workshopData),
		})
	}

	return documents, nil
}

func (m *WorkshopDefinitionManager) exportTrainingPortalWithWorkshopSelection(directory string, workshops []BundleWorkshop) ([]byte, error) {
	trainingPortalPath := filepath.Join(directory, constants.DefaultTrainingPortalDefinitionPath)
	trainingPortalData, err := os.ReadFile(trainingPortalPath)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot open training portal definition %q", trainingPortalPath)
	}

	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDecoder()
	trainingPortal := &unstructured.Unstructured{}

	if err := runtime.DecodeInto(decoder, trainingPortalData, trainingPortal); err != nil {
		return nil, errors.Wrap(err, "couldn't parse training portal definition")
	}

	if trainingPortal.GetAPIVersion() != constants.EducatesTrainingAPIGroupVersion || trainingPortal.GetKind() != "TrainingPortal" {
		return nil, errors.New("invalid type for training portal definition")
	}

	selectedWorkshops := make([]interface{}, 0, len(workshops))
	for _, workshop := range workshops {
		selectedWorkshops = append(selectedWorkshops, map[string]interface{}{
			"name": workshop.Name,
		})
	}

	unstructured.SetNestedSlice(trainingPortal.Object, selectedWorkshops, "spec", "workshops")

	return yaml.Marshal(&trainingPortal.Object)
}

func (m *WorkshopDefinitionManager) updateTrainingPortalWorkshops(directory string, workshopNames []string) error {
	trainingPortalData, err := m.exportTrainingPortalWithWorkshopSelection(directory, mapWorkshopsFromNames(directory, workshopNames))
	if err != nil {
		return err
	}

	trainingPortalPath := filepath.Join(directory, constants.DefaultTrainingPortalDefinitionPath)
	if err := os.WriteFile(trainingPortalPath, trainingPortalData, 0644); err != nil {
		return errors.Wrapf(err, "unable to update training portal definition %q", trainingPortalPath)
	}

	return nil
}

func mapWorkshopsFromNames(directory string, workshopNames []string) []BundleWorkshop {
	workshops := make([]BundleWorkshop, 0, len(workshopNames))
	for _, workshopName := range workshopNames {
		workshops = append(workshops, BundleWorkshop{
			Name:      workshopName,
			Directory: filepath.Join(directory, constants.DefaultWorkshopsDirectoryName, workshopName),
		})
	}
	return workshops
}
