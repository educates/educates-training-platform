package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

type ExportedYAMLDocument struct {
	Name string
	Data []byte
}

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

func SanitizeTrainingPortalResourceForExport(resource *unstructured.Unstructured) *unstructured.Unstructured {
	exported := resource.DeepCopy()

	unstructured.RemoveNestedField(exported.Object, "spec", "portal", "password")

	return exported
}

func RenderResourceAsYAMLDocument(resource *unstructured.Unstructured) ([]byte, error) {
	jsonData, err := json.MarshalIndent(resource.Object, "", "  ")
	if err != nil {
		return nil, err
	}

	return yaml.JSONToYAML(jsonData)
}

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
