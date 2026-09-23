package cmd

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"
)

// resolveDockerPackageDelivery settles how extension packages declared as an
// image reach this deploy, given what the author asked for and what the local
// daemon can do.
//
// podmanImageAbsent says that the deploy is going to Podman and at least one
// package image is not in the local image store. Neither podman-compose nor
// Podman pulls the image behind a volume, so mounting there would fail at
// container create; a slower start beats a container which cannot be created.
//
// Forcing a mount on a daemon which cannot do it fails here rather than later,
// because the alternative is an opaque failure once Compose is already running.
func resolveDockerPackageDelivery(
	delivery dockerPackageDelivery,
	capabilities daemonCapabilities,
	podmanImageAbsent bool,
) (bool, string, error) {
	if delivery == dockerPackageDeliveryFetch {
		return false, "Extension package images will be fetched.", nil
	}

	supported, reason := daemonSupportsImageMounts(capabilities)

	if delivery == dockerPackageDeliveryImageMount {
		if podmanImageAbsent {
			return false, "", errors.New(
				"cannot mount extension package images because Podman does not pull the image behind a volume " +
					"and it is not in the local image store\n\n" +
					"Pull the image first, or use --package-delivery fetch.",
			)
		}

		if !supported {
			return false, "", errors.Errorf(
				"cannot mount extension package images because %s\n\n"+
					"Docker Engine %d.%d or newer with the containerd image store and Docker Compose %d.%d "+
					"or newer are required, or use --package-delivery fetch.",
				reason, minDockerServerMajor, minDockerServerMinor, minComposeMajor, minComposeMinor,
			)
		}

		return true, "Extension package images will be mounted.", nil
	}

	// Auto.
	if podmanImageAbsent {
		return false, "Extension package images will be fetched because Podman does not pull the image " +
			"behind a volume and it is not in the local image store.", nil
	}

	if !supported {
		return false, fmt.Sprintf(
			"Extension package images will be fetched because %s.", reason,
		), nil
	}

	return true, "Extension package images will be mounted.", nil
}

// describePackageDelivery reports what happened to each package declared as
// an image. Every such package gets a line, including one which contributes
// nothing to any downloaded config, so the author can see it was handled.
func describePackageDelivery(names []string, mounting bool) []string {
	if len(names) == 0 {
		return nil
	}

	delivery := "package fetch"

	if mounting {
		delivery = "image mount"
	}

	lines := make([]string, 0, len(names))

	for _, name := range names {
		lines = append(lines, fmt.Sprintf("Package %s: %s", name, delivery))
	}

	return lines
}

// missingCredentialsMessage explains a pull which failed for want of
// credentials, naming the image, the registry and the command to run. A
// docker deploy has no secret support, so the author's own docker login is
// the only way in.
func missingCredentialsMessage(description string, reference string, host string) string {
	return fmt.Sprintf(
		"unable to pull the %s %s\n\n"+
			"Docker has no credentials for %s. Run \"docker login %s\" and try again. "+
			"Credentials given in the workshop definition, such as the pullSecretRef of an extension "+
			"package, are not used when deploying with Docker.",
		description, reference, host, host,
	)
}

// registryHostForReference returns the registry an image reference names.
// A reference with no registry component is a Docker Hub one, which is what
// the daemon needs credentials for.
func registryHostForReference(reference string) string {
	name, _, found := strings.Cut(reference, "/")

	// A registry is recognised by carrying a dot or a port, or by being
	// localhost; anything else is a Docker Hub namespace.
	if !found || (!strings.ContainsAny(name, ".:") && name != "localhost") {
		return "docker.io"
	}

	return name
}
