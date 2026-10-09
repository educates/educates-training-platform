package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/moby/moby/client"
	"github.com/pkg/errors"
)

// dockerPackageDelivery is how an extension package declared as an image
// reaches a workshop deployed with the docker renderer.
type dockerPackageDelivery string

const (
	// dockerPackageDeliveryAuto mounts where the daemon can and fetches where
	// it cannot.
	dockerPackageDeliveryAuto dockerPackageDelivery = "auto"

	// dockerPackageDeliveryImageMount mounts regardless of what the probe
	// reports, refusing to deploy where the daemon cannot.
	dockerPackageDeliveryImageMount dockerPackageDelivery = "image-mount"

	// dockerPackageDeliveryFetch always fetches, which works anywhere.
	dockerPackageDeliveryFetch dockerPackageDelivery = "fetch"
)

// Mounting an image as a volume needs a daemon and a Compose which implement
// it. Versions are rarely the binding constraint now, but a daemon upgraded
// in place from an older release can be arbitrarily new and still keep the
// image store which cannot do it, which is why the store is probed separately.
const (
	minDockerServerMajor = 28
	minDockerServerMinor = 0

	minComposeMajor = 2
	minComposeMinor = 35
)

// parseDockerPackageDelivery reads the value of the --package-delivery flag.
// An empty value is the default rather than an error, so the flag can be left
// unset.
func parseDockerPackageDelivery(value string) (dockerPackageDelivery, error) {
	switch value {
	case "":
		return dockerPackageDeliveryAuto, nil

	case string(dockerPackageDeliveryAuto):
		return dockerPackageDeliveryAuto, nil

	case string(dockerPackageDeliveryImageMount):
		return dockerPackageDeliveryImageMount, nil

	case string(dockerPackageDeliveryFetch):
		return dockerPackageDeliveryFetch, nil

	default:
		return "", errors.Errorf(
			"invalid package delivery %q, must be one of auto, image-mount or fetch", value,
		)
	}
}

// daemonCapabilities is what the probe reads to decide whether the local
// daemon can mount an image as a volume.
type daemonCapabilities struct {
	// ServerVersion is the daemon's own version, as reported by the API.
	ServerVersion string

	// ComposeVersion is the version of the compose plugin, which is read
	// separately because it is a client side component. It may be empty when
	// the plugin could not be asked.
	ComposeVersion string

	// ContainerdStore reports whether the daemon stores images with the
	// containerd snapshotter, which an image volume requires.
	ContainerdStore bool

	// Podman reports whether the daemon behind the Docker API is Podman,
	// which mounts an image volume but never pulls the image behind one.
	Podman bool
}

// daemonSupportsImageMounts reports whether the probed daemon can mount an
// image as a volume, explaining what is missing when it cannot. The
// explanation is shown to the author, so it names the requirement rather than
// the check which failed.
func daemonSupportsImageMounts(capabilities daemonCapabilities) (bool, string) {
	major, minor, ok := parseDockerVersion(capabilities.ServerVersion)

	if !ok {
		return false, fmt.Sprintf(
			"the Docker Engine version %q could not be read", capabilities.ServerVersion,
		)
	}

	if !atLeastVersion(major, minor, minDockerServerMajor, minDockerServerMinor) {
		return false, fmt.Sprintf(
			"Docker Engine %d.%d is older than %d.%d",
			major, minor, minDockerServerMajor, minDockerServerMinor,
		)
	}

	// The compose plugin is asked separately and may not answer. That is not
	// a reason to refuse a daemon which is otherwise capable: Compose reports
	// its own error if it turns out it cannot mount.
	if composeMajor, composeMinor, ok := parseDockerVersion(capabilities.ComposeVersion); ok {
		if !atLeastVersion(composeMajor, composeMinor, minComposeMajor, minComposeMinor) {
			return false, fmt.Sprintf(
				"Docker Compose %d.%d is older than %d.%d",
				composeMajor, composeMinor, minComposeMajor, minComposeMinor,
			)
		}
	}

	// Checked last because it is the one a version cannot predict, so its
	// message is the one an author with a current daemon is most likely to
	// see.
	if !capabilities.ContainerdStore {
		return false, "the daemon is not using the containerd image store"
	}

	return true, ""
}

// atLeastVersion reports whether a major.minor pair is the same as or newer
// than the one required.
func atLeastVersion(major int, minor int, wantMajor int, wantMinor int) bool {
	if major != wantMajor {
		return major > wantMajor
	}

	return minor >= wantMinor
}

// parseDockerVersion reads a major.minor prefix, ignoring a leading "v" and
// any build suffix. Daemons report versions such as "28.1.1+azure-2" and
// "27.5.1-rd".
func parseDockerVersion(value string) (int, int, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "v")

	if value == "" {
		return 0, 0, false
	}

	parts := strings.SplitN(value, ".", 3)

	if len(parts) < 2 {
		return 0, 0, false
	}

	major, err := strconv.Atoi(parts[0])

	if err != nil {
		return 0, 0, false
	}

	// The minor component carries the suffix when there is no patch number.
	minorText := parts[1]

	for index, character := range minorText {
		if character < '0' || character > '9' {
			minorText = minorText[:index]
			break
		}
	}

	minor, err := strconv.Atoi(minorText)

	if err != nil {
		return 0, 0, false
	}

	return major, minor, true
}

// containerdStoreInUse reports whether the daemon's storage driver status
// names the containerd snapshotter. The driver name itself is not the signal:
// a daemon using the snapshotter reports a driver of "overlayfs", which is
// easily confused with the classic "overlay2" graph driver.
func containerdStoreInUse(driverStatus [][2]string) bool {
	for _, pair := range driverStatus {
		if pair[0] == "driver-type" && strings.HasPrefix(pair[1], "io.containerd.snapshotter") {
			return true
		}
	}

	return false
}

// probeDaemonCapabilities reads what the local daemon reports about itself.
// A failure to read any one value leaves it empty rather than failing the
// deploy, since the probe only decides how packages are delivered and there
// is always a delivery which works.
func probeDaemonCapabilities(ctx context.Context, cli *client.Client) daemonCapabilities {
	capabilities := daemonCapabilities{}

	if version, err := cli.ServerVersion(ctx, client.ServerVersionOptions{}); err == nil {
		capabilities.ServerVersion = version.Version
		capabilities.Podman = describesPodman(version)
	}

	if info, err := cli.Info(ctx, client.InfoOptions{}); err == nil {
		capabilities.ContainerdStore = containerdStoreInUse(info.Info.DriverStatus)
	}

	capabilities.ComposeVersion = composeVersion(ctx)

	return capabilities
}

// describesPodman reports whether a version response describes Podman serving
// the Docker API, which it announces in the platform name and its components.
func describesPodman(version client.ServerVersionResult) bool {
	if strings.Contains(strings.ToLower(version.Platform.Name), "podman") {
		return true
	}

	for _, component := range version.Components {
		if strings.Contains(strings.ToLower(component.Name), "podman") {
			return true
		}
	}

	return false
}

// composeVersion asks the compose plugin its version. An empty string means
// it could not be asked, which the support check treats as unknown rather
// than unsupported.
func composeVersion(ctx context.Context) string {
	output, err := exec.CommandContext(ctx, "docker", "compose", "version", "--short").Output()

	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}
