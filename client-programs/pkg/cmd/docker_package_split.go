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

// The image pull policies. Always and Never change what the docker renderer
// does, while IfNotPresent leaves Compose to pull the image when it is
// missing, which is what it asks for.
const (
	imagePullPolicyAlways       = "Always"
	imagePullPolicyNever        = "Never"
	imagePullPolicyIfNotPresent = "IfNotPresent"
)

// imageRepositories are the two addresses $(image_repository) stands for in a
// docker deploy. The session reaches the local registry over the educates
// network by a name only that network resolves, while the Docker daemon, which
// pulls every image it mounts, reaches the same registry through the port
// published on the host.
type imageRepositories struct {
	// Session is the address used by processes in the session, such as the
	// package fetcher.
	Session string

	// Daemon is the address used by the Docker daemon.
	Daemon string
}

// imagePackage is an extension package declared as an image, read from the
// workshop definition once so that neither the delivery nor the pull policy
// has to walk the declaration again.
type imagePackage struct {
	// Name is the package name, which is also the directory it is delivered
	// to.
	Name string

	// Path is where the package lands in the session.
	Path string

	// Reference is the image reference with the three tokens expanded for the
	// session, which is what the package fetcher pulls.
	Reference string

	// DaemonReference is the same reference expanded for the Docker daemon,
	// which is what a mount, a pull and a presence check through the daemon
	// use.
	DaemonReference string

	// PullPolicy is the declared imagePullPolicy, or the one Kubernetes would
	// give the image volume when none was declared.
	PullPolicy string
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

// readImagePackages reads the extension packages declared as an image.
// Packages declaring files are left alone: vendir downloads those, exactly as
// before.
func readImagePackages(
	workshop *unstructured.Unstructured,
	repositories imageRepositories,
	name string,
	version string,
) ([]imagePackage, error) {
	var packages []imagePackage

	workshopVersion, found, _ := unstructured.NestedString(workshop.Object, "spec", "version")

	if !found {
		workshopVersion = version
	}

	packagesItems, found, _ := unstructured.NestedSlice(workshop.Object, "spec", "workshop", "packages")

	if !found || len(packagesItems) == 0 {
		return nil, nil
	}

	for _, packagesItem := range packagesItems {
		item, ok := packagesItem.(map[string]interface{})

		if !ok {
			return nil, errors.New("unable to parse extension package, entry is not an object")
		}

		tmpName, found := item["name"]

		if !found {
			continue
		}

		packageName, ok := tmpName.(string)

		if !ok {
			return nil, errors.Errorf("unable to parse extension package, name %v is not a string", tmpName)
		}

		tmpImage, found := item["image"]

		if !found || tmpImage == nil {
			continue
		}

		packageImage, ok := tmpImage.(string)

		if !ok {
			return nil, errors.Errorf(
				"unable to parse extension package %q, image is not a string", packageName,
			)
		}

		pullPolicy := ""

		if tmpPolicy, found := item["imagePullPolicy"]; found && tmpPolicy != nil {
			pullPolicy, ok = tmpPolicy.(string)

			if !ok {
				return nil, errors.Errorf(
					"unable to parse extension package %q, imagePullPolicy is not a string", packageName,
				)
			}
		}

		daemonReference := expandPackageImageTokens(packageImage, repositories.Daemon, name, workshopVersion)

		// The default is taken from the expanded reference, since the
		// workshop version token can expand to a latest tag.
		if pullPolicy == "" {
			pullPolicy = defaultPackagePullPolicy(daemonReference)
		}

		packages = append(packages, imagePackage{
			Name:            packageName,
			Path:            filepath.Clean(path.Join("/opt/packages", packageName)),
			Reference:       expandPackageImageTokens(packageImage, repositories.Session, name, workshopVersion),
			DaemonReference: daemonReference,
			PullPolicy:      pullPolicy,
		})
	}

	return packages, nil
}

// deliverImagePackages sorts the packages into the chosen delivery. Mounting
// produces compose volumes and no fetcher entries, and fetching the reverse,
// because the base image reads the absence of a fetcher entry as meaning the
// package was mounted. A volume names the image as the daemon reaches it,
// since the daemon is what pulls it.
func deliverImagePackages(packages []imagePackage, mounting bool) splitPackages {
	delivered := splitPackages{}

	for _, entry := range packages {
		delivered.Names = append(delivered.Names, entry.Name)

		if mounting {
			delivered.Mounts = append(delivered.Mounts, composetypes.ServiceVolumeConfig{
				Type:     "image",
				Source:   entry.DaemonReference,
				Target:   entry.Path,
				ReadOnly: true,
			})

			continue
		}

		delivered.Fetches = append(delivered.Fetches, fetchPackageEntry{
			Path:  entry.Path,
			Image: entry.Reference,
		})
	}

	return delivered
}

// splitImagePackages reads the declaration and delivers it in one step, for
// callers which do not need the packages themselves.
func splitImagePackages(
	workshop *unstructured.Unstructured,
	mounting bool,
	repositories imageRepositories,
	name string,
	version string,
) (splitPackages, error) {
	packages, err := readImagePackages(workshop, repositories, name, version)

	if err != nil {
		return splitPackages{}, err
	}

	return deliverImagePackages(packages, mounting), nil
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
