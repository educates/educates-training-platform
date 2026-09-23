package cmd

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	dockerref "github.com/distribution/reference"
	"github.com/moby/moby/client"
	"github.com/pkg/errors"
)

// What each kind of image is called in the messages a pull prints.
const (
	workshopImageDescription = "workshop image"
	packageImageDescription  = "extension package image"
)

// workshopImagePullPolicy returns the pull policy the session manager gives a
// workshop image on a cluster, so a local deploy refreshes the same images a
// cluster would. A tag which is expected to move, or no tag at all, is pulled
// every time, and any other tag only when the image is missing.
func workshopImagePullPolicy(image string) string {
	for _, tag := range []string{":main", ":master", ":develop", ":latest"} {
		if strings.HasSuffix(image, tag) {
			return imagePullPolicyAlways
		}
	}

	if !strings.Contains(image, ":") {
		return imagePullPolicyAlways
	}

	return imagePullPolicyIfNotPresent
}

// defaultPackagePullPolicy returns the pull policy Kubernetes gives an image
// volume which declares none: Always when the tag is latest, which a reference
// with neither a tag nor a digest implies, and IfNotPresent otherwise.
func defaultPackagePullPolicy(reference string) string {
	named, err := dockerref.ParseNormalizedNamed(reference)

	if err != nil {
		return imagePullPolicyIfNotPresent
	}

	tag := ""

	if tagged, ok := named.(dockerref.Tagged); ok {
		tag = tagged.Tag()
	}

	_, digested := named.(dockerref.Digested)

	if tag == "latest" || (tag == "" && !digested) {
		return imagePullPolicyAlways
	}

	return imagePullPolicyIfNotPresent
}

// refreshImage pulls an image whose pull policy is Always. A failed pull falls
// back to a copy already held locally, with a warning, so a deploy still works
// offline or with an image loaded straight into the daemon rather than pushed
// to a registry. Only when there is no local copy does the failure stop the
// deploy.
func refreshImage(
	reference string,
	description string,
	pull func(string) error,
	present func(string) bool,
	stdout io.Writer,
) error {
	fmt.Fprintf(stdout, "Pulling %s %s\n", description, reference)

	err := pull(reference)

	if err == nil {
		return nil
	}

	if !present(reference) {
		return err
	}

	fmt.Fprintf(stdout, "Warning: using the %s %s already held locally: %v\n", description, reference, err)

	return nil
}

// refreshImageWithDocker refreshes an image through the local Docker install.
func refreshImageWithDocker(
	ctx context.Context,
	cli *client.Client,
	reference string,
	description string,
	stdout io.Writer,
) error {
	return refreshImage(
		reference,
		description,
		func(reference string) error {
			return pullImage(ctx, reference, description)
		},
		func(reference string) bool {
			return imagePresentLocally(ctx, cli, reference)
		},
		stdout,
	)
}

// pullImage pulls an image with the docker command rather than through the
// API, so that the pull uses the credentials "docker login" stored, including
// those kept by a credential helper. A failure for want of credentials is
// reported in a way the author can act on.
func pullImage(ctx context.Context, reference string, description string) error {
	output, err := exec.CommandContext(ctx, "docker", "pull", "--quiet", reference).CombinedOutput()

	if err == nil {
		return nil
	}

	message := strings.TrimSpace(string(output))

	if message == "" {
		message = err.Error()
	}

	if isAuthenticationFailure(errors.New(message)) {
		return errors.New(missingCredentialsMessage(description, reference, registryHostForReference(reference)))
	}

	return errors.Errorf("unable to pull the %s %s: %s", description, reference, message)
}
