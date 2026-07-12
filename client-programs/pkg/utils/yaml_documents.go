package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// ExportedYAMLDocument is a single named YAML document produced by an
// export. Name is used as the file name when writing to a directory; Data
// is the YAML body.
type ExportedYAMLDocument struct {
	Name string
	Data []byte
}

// WorkshopResourceExportConfig controls how a Workshop resource is rewritten
// on export.
type WorkshopResourceExportConfig struct {
	Repository      string
	WorkshopVersion string
}

// SanitizeResourceForExport returns a deep copy of resource with the
// cluster-managed fields removed (status, and the server-populated
// metadata fields) so the result is a clean, re-appliable manifest.
func SanitizeResourceForExport(resource *unstructured.Unstructured) *unstructured.Unstructured {
	exported := resource.DeepCopy()

	unstructured.RemoveNestedField(exported.Object, "status")

	metadata, found, _ := unstructured.NestedMap(exported.Object, "metadata")
	if found {
		if annotations, ok := metadata["annotations"].(map[string]interface{}); ok {
			delete(annotations, "kopf.zalando.org/last-handled-configuration")
			delete(annotations, "training.educates.dev/source")
			delete(annotations, "training.educates.dev/workshop")

			if len(annotations) == 0 {
				delete(metadata, "annotations")
			} else {
				metadata["annotations"] = annotations
			}
		}

		delete(metadata, "creationTimestamp")
		delete(metadata, "deletionGracePeriodSeconds")
		delete(metadata, "deletionTimestamp")
		delete(metadata, "generateName")
		delete(metadata, "generation")
		delete(metadata, "managedFields")
		delete(metadata, "resourceVersion")
		delete(metadata, "selfLink")
		delete(metadata, "uid")

		if len(metadata) == 0 {
			unstructured.RemoveNestedField(exported.Object, "metadata")
		} else {
			unstructured.SetNestedMap(exported.Object, metadata, "metadata")
		}
	}

	return exported
}

// SanitizeTrainingPortalResourceForExport removes portal fields that should
// not be carried across clusters, in addition to the common sanitization
// (currently the generated portal password).
func SanitizeTrainingPortalResourceForExport(resource *unstructured.Unstructured) *unstructured.Unstructured {
	exported := resource.DeepCopy()

	unstructured.RemoveNestedField(exported.Object, "spec", "portal", "password")

	return exported
}

// SanitizeWorkshopResourceForExport rewrites a Workshop resource for export:
// it substitutes the $(image_repository) and $(workshop_version) variables
// with the supplied values, records the version under spec.version when it
// isn't already set (and isn't "latest"), and drops the spec.publish block
// (a build-time concern that shouldn't ship in an exported workshop).
func SanitizeWorkshopResourceForExport(resource *unstructured.Unstructured, cfg *WorkshopResourceExportConfig) *unstructured.Unstructured {
	exported := resource.DeepCopy()

	repository := ""
	workshopVersion := "latest"

	if cfg != nil {
		repository = cfg.Repository
		if cfg.WorkshopVersion != "" {
			workshopVersion = cfg.WorkshopVersion
		}
	}

	exported.Object = replaceWorkshopExportVariables(exported.Object, repository, workshopVersion).(map[string]interface{})

	_, found, _ := unstructured.NestedString(exported.Object, "spec", "version")
	if !found && workshopVersion != "latest" {
		unstructured.SetNestedField(exported.Object, workshopVersion, "spec", "version")
	}

	unstructured.RemoveNestedField(exported.Object, "spec", "publish")

	return exported
}

func replaceWorkshopExportVariables(value interface{}, repository string, workshopVersion string) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		for key, nested := range typed {
			typed[key] = replaceWorkshopExportVariables(nested, repository, workshopVersion)
		}
		return typed
	case []interface{}:
		for index, nested := range typed {
			typed[index] = replaceWorkshopExportVariables(nested, repository, workshopVersion)
		}
		return typed
	case string:
		replaced := typed
		if repository != "" {
			replaced = strings.ReplaceAll(replaced, "$(image_repository)", repository)
		}
		if workshopVersion != "" {
			replaced = strings.ReplaceAll(replaced, "$(workshop_version)", workshopVersion)
		}
		return replaced
	default:
		return value
	}
}

// RenderResourceAsYAMLDocument marshals an unstructured resource to a YAML
// document.
func RenderResourceAsYAMLDocument(resource *unstructured.Unstructured) ([]byte, error) {
	jsonData, err := json.MarshalIndent(resource.Object, "", "  ")
	if err != nil {
		return nil, err
	}

	return yaml.JSONToYAML(jsonData)
}

// PrintExportedDocuments writes the documents to stdout as a single YAML
// stream, separating them with '---'.
func PrintExportedDocuments(documents []ExportedYAMLDocument) error {
	var output bytes.Buffer

	for i, doc := range documents {
		if i != 0 {
			output.WriteString("---\n")
		}
		output.Write(doc.Data)
		if len(doc.Data) > 0 && doc.Data[len(doc.Data)-1] != '\n' {
			output.WriteString("\n")
		}
	}

	fmt.Print(output.String())

	return nil
}

// WriteExportedDocuments writes each document to dir/<doc.Name>, creating
// the directory if needed.
func WriteExportedDocuments(dir string, documents []ExportedYAMLDocument) error {
	outputDirectory := filepath.Clean(dir)

	if err := os.MkdirAll(outputDirectory, os.ModePerm); err != nil {
		return errors.Wrapf(err, "unable to create export directory %q", outputDirectory)
	}

	for _, doc := range documents {
		targetPath := filepath.Join(outputDirectory, doc.Name)

		if err := os.WriteFile(targetPath, doc.Data, 0644); err != nil {
			return errors.Wrapf(err, "unable to write export file %q", targetPath)
		}
	}

	return nil
}
