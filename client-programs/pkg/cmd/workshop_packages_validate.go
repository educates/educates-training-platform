package cmd

import (
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// The docker renderer has no schema for the Workshop definition, so the rules
// the Workshop custom resource definition enforces through CEL and OpenAPI are
// repeated here in code. The message text matches the custom resource
// definition so an author gets the same verdict from a local deploy as from a
// cluster.

var (
	// packageImageReferencePattern accepts a tagged reference and rejects a
	// digest, mirroring the pattern on the custom resource definition.
	packageImageReferencePattern = regexp.MustCompile(`^([^/@]+/)*[^/@:]+:[^/@:]+$`)

	// packageNameDNSLabelPattern is the name rule applied to a package
	// delivered as an image.
	packageNameDNSLabelPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,55}[a-z0-9])?$`)

	// packageImagePullPolicies are the accepted values, matching the enum on
	// the custom resource definition.
	packageImagePullPolicies = []string{"Always", "Never", "IfNotPresent"}

	// packageImageReservedVariables may appear in a workshop definition but
	// never in a package image reference.
	packageImageReservedVariables = []string{"$(platform_arch)", "$(oci_image_cache)"}
)

const (
	packageImageReferenceMaxLength = 256
	packageNameMaxLength           = 63
)

// validateWorkshopPackages reports the first extension package in the workshop
// definition which would be rejected by the cluster.
func validateWorkshopPackages(workshop *unstructured.Unstructured) error {
	packagesItems, found, err := unstructured.NestedSlice(workshop.Object, "spec", "workshop", "packages")

	if err != nil {
		return errors.Wrap(err, "unable to parse extension packages")
	}

	if !found {
		return nil
	}

	for _, packagesItem := range packagesItems {
		item, ok := packagesItem.(map[string]interface{})

		if !ok {
			return errors.New("unable to parse extension package, entry is not an object")
		}

		if err := validateWorkshopPackage(item); err != nil {
			return err
		}
	}

	return nil
}

func validateWorkshopPackage(item map[string]interface{}) error {
	name, err := packageStringProperty(item, "name")

	if err != nil {
		return err
	}

	if len(name) > packageNameMaxLength {
		return errors.Errorf("extension package name %q is longer than %d characters", name, packageNameMaxLength)
	}

	image, err := packageStringProperty(item, "image")

	if err != nil {
		return err
	}

	_, hasFiles := item["files"]
	hasImage := image != ""

	if hasFiles == hasImage {
		return errors.New("an extension package declares either files or image, not both")
	}

	pullPolicy, err := packageStringProperty(item, "imagePullPolicy")

	if err != nil {
		return err
	}

	if pullPolicy != "" {
		if !hasImage {
			return errors.New("imagePullPolicy requires image")
		}

		if !contains(packageImagePullPolicies, pullPolicy) {
			return errors.Errorf("imagePullPolicy %q must be one of %s", pullPolicy, strings.Join(packageImagePullPolicies, ", "))
		}
	}

	if _, found := item["pullSecretRef"]; found && !hasImage {
		return errors.New("pullSecretRef requires image")
	}

	if !hasImage {
		return nil
	}

	for _, variable := range packageImageReservedVariables {
		if strings.Contains(image, variable) {
			return errors.New("image must not use the reserved $(platform_arch) or $(oci_image_cache) variables")
		}
	}

	if len(image) > packageImageReferenceMaxLength {
		return errors.Errorf("image reference for extension package %q is longer than %d characters", name, packageImageReferenceMaxLength)
	}

	if !packageImageReferencePattern.MatchString(image) {
		return errors.New("image must be a tagged reference with no digest")
	}

	if !packageNameDNSLabelPattern.MatchString(name) {
		return errors.New("an extension package delivered as an image needs a DNS label name")
	}

	return nil
}

// packageStringProperty reads an optional string property, reporting a value
// of the wrong type rather than ignoring it.
func packageStringProperty(item map[string]interface{}, property string) (string, error) {
	value, found := item[property]

	if !found || value == nil {
		return "", nil
	}

	text, ok := value.(string)

	if !ok {
		return "", errors.Errorf("unable to parse extension package, %s %v is not a string", property, value)
	}

	return text, nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}

	return false
}
