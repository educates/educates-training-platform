package localclusters

import (
	"errors"
	"fmt"
	"slices"
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
// creating or starting id would reuse that container as it is. The error
// names each such mirror with the values its container has and those
// declared.
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
				"'educates local mirror delete %s', which unlinks it from every local cluster, and run the command again",
				id.Description(), d.Mirror, declaredValues, containerValues, d.Mirror))
		}
	}
	return errors.Join(errs...)
}

// MirrorsToLink returns the registry mirrors that local cluster create
// and start link into every node of a local cluster whose config declares
// the mirrors declared: every registry mirror container that exists,
// whichever cluster's config or command created it, and every mirror
// declared, whose container is created first when it is missing. Each
// mirror comes once, sorted by name. A declared mirror carries its
// declaration, password included, which CheckMirrors has matched with
// its container; any other carries the values its container's labels
// give, which is all linking it needs.
func (inv Inventory) MirrorsToLink(declared []v1alpha1.RegistryMirror) []v1alpha1.RegistryMirror {
	var mirrors []v1alpha1.RegistryMirror
	seen := map[string]bool{}
	for _, d := range declared {
		if !seen[d.Mirror] {
			seen[d.Mirror] = true
			mirrors = append(mirrors, d)
		}
	}
	for _, c := range inv.observed.Mirrors {
		if !seen[c.Mirror] {
			seen[c.Mirror] = true
			mirrors = append(mirrors, v1alpha1.RegistryMirror{Mirror: c.Mirror, URL: c.URL, Username: c.Username})
		}
	}
	slices.SortFunc(mirrors, func(a, b v1alpha1.RegistryMirror) int {
		return strings.Compare(a.Mirror, b.Mirror)
	})
	return mirrors
}

// ClustersDeclaringMirror returns the Educates clusters, running or
// stopped and in the order of Clusters, whose config in the data home
// declares the registry mirror mirror. Their next local cluster start,
// or create once deleted, creates the mirror's container again, so local
// mirror delete names them. A cluster whose config is missing or does not
// load declares nothing.
func (inv Inventory) ClustersDeclaringMirror(mirror string) []Identity {
	var ids []Identity
	for _, id := range inv.clusters {
		loaded, err := LoadConfig(id)
		if err != nil {
			continue
		}
		if slices.ContainsFunc(loaded.RegistryMirrors(), func(m v1alpha1.RegistryMirror) bool {
			return m.Mirror == mirror
		}) {
			ids = append(ids, id)
		}
	}
	return ids
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
