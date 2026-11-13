package localclusters

import (
	"fmt"
	"strconv"
	"strings"
)

// PrivilegedPortForwarding is Docker Desktop's setting "Allow privileged
// port forwarding to external machines", as the command line tool reads
// it.
type PrivilegedPortForwarding int

const (
	// PrivilegedPortForwardingUnknown is a setting that could not be read.
	PrivilegedPortForwardingUnknown PrivilegedPortForwarding = iota

	// PrivilegedPortForwardingOn is the setting turned on.
	PrivilegedPortForwardingOn

	// PrivilegedPortForwardingOff is the setting turned off.
	PrivilegedPortForwardingOff
)

// privilegedPortSettingPath is where Docker Desktop shows the setting.
const privilegedPortSettingPath = "Settings > Advanced > Allow privileged port forwarding to external machines"

// dockerDesktopPlatform is how the Docker API names Docker Desktop's
// platform, followed by its version, as in "Docker Desktop 4.97.0".
const dockerDesktopPlatform = "Docker Desktop "

// CheckPrivilegedPortForwarding checks that the Docker engine can serve a
// local cluster's ports 80 and 443 on its listen address, a non-loopback
// address. platform is the Docker server's platform name, as the Docker
// API reports it, such as "Docker Desktop 4.97.0", and setting is Docker
// Desktop's setting "Allow privileged port forwarding to external
// machines".
//
// Docker Desktop 4.97 and later serves ports below 1024 on loopback
// addresses only while the setting is off, so it refuses then, naming the
// setting. With the setting unknown it returns a warning instead. Other
// Docker engines, and earlier Docker Desktop releases, need no setting.
func CheckPrivilegedPortForwarding(platform string, setting PrivilegedPortForwarding) (string, error) {
	if !needsPrivilegedPortForwarding(platform) {
		return "", nil
	}
	why := fmt.Sprintf("%s serves ports below 1024 on loopback addresses only while its setting "+
		"\"Allow privileged port forwarding to external machines\" is off, and the cluster's ports 80 and 443 "+
		"on a non-loopback address need it", platform)
	switch setting {
	case PrivilegedPortForwardingOn:
		return "", nil
	case PrivilegedPortForwardingOff:
		return "", fmt.Errorf("%s. Turn it on in %s, then run the command again", why, privilegedPortSettingPath)
	default:
		return fmt.Sprintf("could not read whether Docker Desktop's setting \"Allow privileged port forwarding to "+
			"external machines\" is on: %s. If the cluster can't be reached, turn it on in %s",
			why, privilegedPortSettingPath), nil
	}
}

// needsPrivilegedPortForwarding reports whether the Docker server named
// platform is Docker Desktop 4.97 or later, which serves ports below 1024
// on a non-loopback address only with the setting on.
func needsPrivilegedPortForwarding(platform string) bool {
	version, ok := strings.CutPrefix(platform, dockerDesktopPlatform)
	fields := strings.Fields(version)
	if !ok || len(fields) == 0 {
		return false
	}
	parts := strings.SplitN(fields[0], ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return false
	}
	return major > 4 || major == 4 && minor >= 97
}
