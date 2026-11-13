package localclusters

import (
	"errors"
	"fmt"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// MirrorContainer is a registry mirror container on the Docker host, as
// the labels the CLI puts on it when it creates it describe it.
type MirrorContainer struct {
	// Mirror is the registry the container mirrors, by the name image
	// references use, such as docker.io.
	Mirror string

	// URL is the remote registry the container pulls from, as given when
	// it was created. Empty means the registry Mirror names.
	URL string

	// Username is the username the container authenticates to the remote
	// registry as, empty for none.
	Username string
}

// CheckMirrors returns an error when a registry mirror among declared,
// the mirrors the config of the local cluster id declares, already has a
// container that pulls from another remote registry or as another
// username. Every local cluster shares one container for each mirror, and
// creating id would reuse that container as it is. The error names each
// such mirror with the values its container has and those declared.
//
// A container's password can't be seen, so a different password is not
// detected.
func (inv Inventory) CheckMirrors(id Identity, declared []v1alpha1.RegistryMirror) error {
	var errs []error
	for _, d := range declared {
		for _, c := range inv.observed.Mirrors {
			if c.Mirror != d.Mirror {
				continue
			}
			want := MirrorContainer{Mirror: d.Mirror, URL: d.URL, Username: d.Username}
			declaredValues, containerValues := want.differences(c)
			if declaredValues == "" {
				continue
			}
			errs = append(errs, fmt.Errorf("the config of %s declares registry mirror %s with %s, "+
				"but the mirror's container, which every local cluster shares, already exists with %s. "+
				"Declare the mirror as its container has it, or delete the container with "+
				"'educates local mirror delete %s', which unlinks it from every local cluster, and create the cluster again",
				id.Description(), d.Mirror, declaredValues, containerValues, d.Mirror))
		}
	}
	return errors.Join(errs...)
}

// differences describes the values of the mirror containers m and other
// that differ, m's first and other's second, or returns two empty strings
// when none does.
func (m MirrorContainer) differences(other MirrorContainer) (string, string) {
	var mine, theirs []string
	if m.remoteURL() != other.remoteURL() {
		mine = append(mine, "URL "+m.remoteURL())
		theirs = append(theirs, "URL "+other.remoteURL())
	}
	if m.Username != other.Username {
		mine = append(mine, describeUsername(m.Username))
		theirs = append(theirs, describeUsername(other.Username))
	}
	return strings.Join(mine, " and "), strings.Join(theirs, " and ")
}

// remoteURL is the remote registry the mirror container m pulls from.
func (m MirrorContainer) remoteURL() string {
	if m.URL == "" {
		return m.Mirror
	}
	return m.URL
}

// describeUsername names the username a mirror authenticates to its
// remote registry as in a message.
func describeUsername(username string) string {
	if username == "" {
		return "no username"
	}
	return "username " + username
}
