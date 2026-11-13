package localclusters

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
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
// the mirrors the registry mirrors file of the data home declares,
// already has a container that pulls from another remote registry or as
// another username. Every local cluster shares one container for each
// mirror, and creating or starting a cluster would reuse that container
// as it is. The error names the file, and each such mirror with the
// values its container has and those declared.
//
// A container's password can't be seen, so a different password is not
// detected.
func (inv Inventory) CheckMirrors(declared []v1alpha1.RegistryMirror) error {
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
			errs = append(errs, fmt.Errorf("%s declares registry mirror %s with %s, "+
				"but the mirror's container, which every local cluster shares, already exists with %s. "+
				"Declare the mirror there as its container has it, or run 'educates local mirror delete %[2]s', "+
				"which deletes the container and the mirror's entry, then deploy the mirror with the values you want "+
				"with 'educates local mirror deploy %[2]s', and run the command again",
				config.LocalMirrorsPath(inv.dataHome), d.Mirror, declaredValues, containerValues))
		}
	}
	return errors.Join(errs...)
}

// MirrorsToLink returns the registry mirrors that local cluster create
// and start link into every node of a local cluster, when the registry
// mirrors file declares the mirrors declared: every registry mirror
// container that exists, whichever command created it, and every mirror
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

// MirrorRecord is what local mirror deploy does for a registry mirror
// once RecordMirror has recorded it.
type MirrorRecord struct {
	// Mirror is the mirror to deploy, as the registry mirrors file records
	// it.
	Mirror v1alpha1.RegistryMirror

	// Note is what local mirror deploy tells the user after deploying the
	// mirror, empty for nothing: that the mirror's container keeps the
	// password it was created with, when RecordMirror replaced the
	// recorded password of a mirror whose container exists.
	Note string
}

// RecordMirror records requested, the registry mirror local mirror deploy
// was given, in the registry mirrors file of the data home, before its
// container is created, and returns the mirror to deploy.
//
// A mirror the file doesn't record is added to it. A mirror it records
// with another URL or username is refused, and the file is left as it
// is: the error says to delete the mirror first. One it records with the
// same URL and username is deployed as recorded, with the password given
// in place of the recorded one, which the file then records. The
// container of a mirror keeps the password it was created with, so when
// it exists and the password changed, the note says so.
func (inv Inventory) RecordMirror(requested v1alpha1.RegistryMirror) (MirrorRecord, error) {
	path := config.LocalMirrorsPath(inv.dataHome)
	recorded, err := config.LoadLocalMirrors(inv.dataHome)
	if err != nil {
		return MirrorRecord{}, err
	}
	want := MirrorContainer{Mirror: requested.Mirror, URL: requested.URL, Username: requested.Username}
	i := slices.IndexFunc(recorded, func(r v1alpha1.RegistryMirror) bool { return r.Mirror == requested.Mirror })
	if i >= 0 {
		have := MirrorContainer{Mirror: recorded[i].Mirror, URL: recorded[i].URL, Username: recorded[i].Username}
		if recordedValues, requestedValues := have.differences(want); recordedValues != "" {
			return MirrorRecord{}, fmt.Errorf("%s records registry mirror %s with %s, not %s as given. "+
				"To change them, run 'educates local mirror delete %s' first, which deletes the mirror's "+
				"container and its entry, and deploy the mirror again",
				path, requested.Mirror, recordedValues, requestedValues, requested.Mirror)
		}
	}
	for _, c := range inv.observed.Mirrors {
		if c.Mirror != requested.Mirror {
			continue
		}
		if containerValues, requestedValues := c.differences(want); containerValues != "" {
			return MirrorRecord{}, fmt.Errorf("the container of registry mirror %s, which every local cluster shares, "+
				"already exists with %s, not %s as given. To change them, run 'educates local mirror delete %s' "+
				"first, which deletes the container, and deploy the mirror again",
				requested.Mirror, containerValues, requestedValues, requested.Mirror)
		}
	}
	if i < 0 {
		if err := config.RecordLocalMirror(inv.dataHome, requested); err != nil {
			return MirrorRecord{}, err
		}
		return MirrorRecord{Mirror: requested}, nil
	}

	entry := recorded[i]
	if requested.Password == "" || requested.Password == entry.Password {
		return MirrorRecord{Mirror: entry}, nil
	}

	entry.Password = requested.Password
	if err := config.RecordLocalMirror(inv.dataHome, entry); err != nil {
		return MirrorRecord{}, err
	}
	record := MirrorRecord{Mirror: entry}
	if inv.hasMirrorContainer(entry.Mirror) {
		record.Note = fmt.Sprintf("recorded the new password of registry mirror %s in %s, but the mirror's "+
			"container keeps the password it was created with. To use the new one, run "+
			"'educates local mirror delete %s', which also deletes the mirror's cached images, and deploy it again",
			entry.Mirror, path, entry.Mirror)
	}
	return record, nil
}

// MirrorRemoval is what local mirror delete does for a registry mirror
// once RemoveMirrorRecord has removed its entry.
type MirrorRemoval struct {
	// HasContainer reports whether the mirror's container exists, which
	// the command then deletes.
	HasContainer bool

	// Note is what the command tells the user when the mirror had no
	// container, so that removing its entry was all there was to do.
	Note string
}

// RemoveMirrorRecord removes the entry of the registry mirror mirror from
// the registry mirrors file of the data home, before local mirror delete
// deletes its container, so that no local cluster created or started
// later needs the mirror any more. A mirror the file doesn't record and
// that has no container is an error; one with only a container leaves the
// file as it is.
func (inv Inventory) RemoveMirrorRecord(mirror string) (MirrorRemoval, error) {
	path := config.LocalMirrorsPath(inv.dataHome)
	recorded, err := config.RemoveLocalMirror(inv.dataHome, mirror)
	if err != nil {
		return MirrorRemoval{}, err
	}
	removal := MirrorRemoval{HasContainer: inv.hasMirrorContainer(mirror)}
	switch {
	case !recorded && !removal.HasContainer:
		return MirrorRemoval{}, fmt.Errorf("registry mirror %s has no container, and %s does not record it",
			mirror, path)
	case !removal.HasContainer:
		removal.Note = fmt.Sprintf("removed registry mirror %s from %s; it had no container", mirror, path)
	}
	return removal, nil
}

// hasMirrorContainer reports whether the registry mirror mirror has a
// container, running or stopped.
func (inv Inventory) hasMirrorContainer(mirror string) bool {
	return slices.ContainsFunc(inv.observed.Mirrors, func(c MirrorContainer) bool { return c.Mirror == mirror })
}

// ListedMirror is a registry mirror local mirror list shows: one whose
// container exists, one the registry mirrors file declares, or both.
type ListedMirror struct {
	// Mirror is the registry the mirror mirrors, such as docker.io.
	Mirror string

	// Declaration is the mirror's entry in the registry mirrors file, nil
	// when the file does not declare it.
	Declaration *v1alpha1.RegistryMirror

	// HasContainer reports whether the mirror's container exists. A
	// declared mirror without one gets it on the next local cluster
	// create or start.
	HasContainer bool
}

// ListMirrors returns the registry mirrors local mirror list shows,
// sorted by name: the mirror of each container among containers, the
// mirrors whose containers exist, and each mirror among declared, the
// mirrors the registry mirrors file declares, whether its container
// exists or not.
func ListMirrors(containers []string, declared []v1alpha1.RegistryMirror) []ListedMirror {
	var listed []ListedMirror
	index := map[string]int{}
	for _, d := range declared {
		if _, seen := index[d.Mirror]; seen {
			continue
		}
		index[d.Mirror] = len(listed)
		listed = append(listed, ListedMirror{Mirror: d.Mirror, Declaration: &d})
	}
	for _, c := range containers {
		if i, seen := index[c]; seen {
			listed[i].HasContainer = true
			continue
		}
		index[c] = len(listed)
		listed = append(listed, ListedMirror{Mirror: c, HasContainer: true})
	}
	slices.SortFunc(listed, func(a, b ListedMirror) int {
		return strings.Compare(a.Mirror, b.Mirror)
	})
	return listed
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
