package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/moby/moby/client"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// resolveImagePackageDelivery settles how the extension packages declared as
// an image reach this deploy and prepares them, returning the compose volumes
// to add and the fetcher entries to write.
//
// It probes the daemon, applies the image pull policy for a mounted package,
// and reports what it chose. A workshop declaring no package images does none
// of this and says nothing.
func resolveImagePackageDelivery(
	ctx context.Context,
	cli *client.Client,
	workshop *unstructured.Unstructured,
	delivery dockerPackageDelivery,
	name string,
	repositories imageRepositories,
	version string,
	stdout io.Writer,
) (splitPackages, error) {
	// The declaration is read once here. Which delivery is chosen decides how
	// these packages are handed to the session, not which packages there are.
	declared, err := readImagePackages(workshop, repositories, name, version)

	if err != nil {
		return splitPackages{}, err
	}

	if len(declared) == 0 {
		return splitPackages{}, nil
	}

	capabilities := daemonCapabilities{}

	// A forced fetch never consults the daemon, so a deploy which is not
	// going to mount does not pay for the probe or fail on it.
	if delivery != dockerPackageDeliveryFetch {
		capabilities = probeDaemonCapabilities(ctx, cli)
	}

	// A package asking to be pulled every time is pulled before anything else
	// looks for it locally. On Podman that is what makes a mount possible at
	// all, since the presence check below would otherwise fall back to a
	// fetch over the very image this was about to pull.
	if delivery != dockerPackageDeliveryFetch {
		if err := prePullImagePackages(ctx, cli, declared, stdout); err != nil {
			return splitPackages{}, err
		}
	}

	// Podman does not pull the image behind a volume, so mounting is only
	// possible for an image which is already local. Whether the daemon is
	// Podman comes from the same probe, so no extra call is made, and a
	// forced fetch which skipped the probe does not start making them.
	podmanImageAbsent := false

	if capabilities.Podman {
		for _, entry := range declared {
			if !imagePresentLocally(ctx, cli, entry.DaemonReference) {
				podmanImageAbsent = true
				break
			}
		}
	}

	mounting, message, err := resolveDockerPackageDelivery(delivery, capabilities, podmanImageAbsent)

	if err != nil {
		return splitPackages{}, err
	}

	// A package which refuses to be pulled has to be there already. Compose
	// would otherwise pull it when mounting, which is the one thing the
	// policy forbids.
	if mounting {
		if err := refuseAbsentImagePackages(ctx, cli, declared); err != nil {
			return splitPackages{}, err
		}
	}

	packages := deliverImagePackages(declared, mounting)

	fmt.Fprintln(stdout, message)

	for _, line := range describePackageDelivery(packages.Names, mounting) {
		fmt.Fprintln(stdout, line)
	}

	return packages, nil
}

// prePullImagePackages pulls every package whose pull policy is Always,
// whether declared or given by default to a latest tag. Compose pulls a volume
// image only when it is missing, so a package whose tag was republished would
// otherwise keep the copy already held, which is the opposite of what an
// author republishing a tag wants.
func prePullImagePackages(
	ctx context.Context,
	cli *client.Client,
	packages []imagePackage,
	stdout io.Writer,
) error {
	for _, entry := range packages {
		if entry.PullPolicy != imagePullPolicyAlways {
			continue
		}

		if err := refreshImageWithDocker(ctx, cli, entry.DaemonReference, packageImageDescription, stdout); err != nil {
			return err
		}
	}

	return nil
}

// refuseAbsentImagePackages reports a package which asked never to be pulled
// and is not held locally, before the deploy rather than once Compose has
// pulled it in spite of the policy.
func refuseAbsentImagePackages(ctx context.Context, cli *client.Client, packages []imagePackage) error {
	for _, entry := range packages {
		if entry.PullPolicy != imagePullPolicyNever {
			continue
		}

		if !imagePresentLocally(ctx, cli, entry.DaemonReference) {
			return errors.Errorf(
				"the extension package image %s is not in the local image store and "+
					"imagePullPolicy is Never", entry.DaemonReference,
			)
		}
	}

	return nil
}

// isAuthenticationFailure reports whether a pull failed because the daemon
// had no credentials for the registry, which is the one failure with a
// specific remedy.
//
// A bare "denied" is deliberately not enough: a registry says that for a
// repository which does not exist as well as for one the daemon may not read,
// and telling someone to log in when their reference is simply wrong sends
// them the wrong way.
func isAuthenticationFailure(err error) bool {
	message := strings.ToLower(err.Error())

	for _, marker := range []string{
		"unauthorized",
		"authentication required",
		"authorization failed",
		"access to the resource is denied",
		"no basic auth credentials",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}

	return false
}

// imagePresentLocally reports whether an image is already in the daemon's
// image store.
func imagePresentLocally(ctx context.Context, cli *client.Client, reference string) bool {
	_, err := cli.ImageInspect(ctx, reference)

	return err == nil
}
