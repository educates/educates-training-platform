package localcerts

import (
	"fmt"
	"slices"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

// ClusterCAKeyPairs is what a local cluster uses of the CA key pairs in
// the local secrets cache, from which deleting another local cluster
// decides which CA key pairs it leaves unused.
type ClusterCAKeyPairs struct {
	// Identity is the local cluster's identity.
	Identity localclusters.Identity

	// Known reports whether the CA key pairs the cluster uses are known.
	// A cluster whose CA key pairs are not known may use any of them.
	Known bool

	// Unknown says why the CA key pairs the cluster uses are not known,
	// to follow "since"; empty when they are known.
	Unknown string

	// Names are the Secrets the cluster uses as CA key pairs, by name.
	// A name that is not a CA key pair's, such as a Secret of the
	// user's own, is never removed from the cache.
	Names []string
}

// CAKeyPairCleanup is which CA key pairs to remove from the local secrets
// cache once a local cluster is deleted, and why any it used are kept.
type CAKeyPairCleanup struct {
	// Remove names the CA key pairs to remove, sorted.
	Remove []string

	// Notices say why CA key pairs the deleted cluster used are kept.
	Notices []string
}

// UsedCachedCAKeyPairs returns the CA key pairs among the cached Secrets
// that the local cluster c uses, sorted and each once: the names of c
// that are CA key pairs' names, as secrets.CAKeyPairName gives them, and
// that the cache holds. It returns none when the CA key pairs c uses are
// not known.
func UsedCachedCAKeyPairs(c ClusterCAKeyPairs, cached []string) []string {
	if !c.Known {
		return nil
	}
	var used []string
	for _, name := range c.Names {
		if role, ok := secrets.ClassifyCachedSecretName(name); !ok || role != secrets.CAKeyPairRole {
			continue
		}
		if slices.Contains(cached, name) && !slices.Contains(used, name) {
			used = append(used, name)
		}
	}
	slices.Sort(used)
	return used
}

// PlanCAKeyPairCleanup decides which CA key pairs to remove from the local
// secrets cache, which holds the Secrets cached, once the local cluster
// deleted is deleted and the local clusters others remain.
//
// It removes each cached CA key pair deleted used that no cluster of
// others uses, and keeps one that a cluster of others uses, or may use
// because what it uses is not known, with a notice saying why. When what
// deleted used is not known, it removes nothing, with a notice when the
// cache holds any CA key pair. A CA key pair is a Secret named as
// secrets.CAKeyPairName names one, which the CLI mints from the root CA
// and `local secrets add` refuses as a name, so a root CA, a TLS
// certificate or any other Secret of the user's is never removed.
func PlanCAKeyPairCleanup(deleted ClusterCAKeyPairs, others []ClusterCAKeyPairs, cached []string) CAKeyPairCleanup {
	var plan CAKeyPairCleanup

	if !deleted.Known {
		if slices.ContainsFunc(cached, isCAKeyPairName) {
			plan.Notices = append(plan.Notices, fmt.Sprintf("Kept the CA key pairs in the local secrets cache: can't tell which one %s used, "+
				"since %s. Remove a CA key pair no local cluster uses with:\n\n  educates local secrets remove NAME",
				deleted.Identity.Description(), deleted.Unknown))
		}
		return plan
	}

	for _, name := range UsedCachedCAKeyPairs(deleted, cached) {
		var users []localclusters.Identity
		var maybe []string
		for _, other := range others {
			switch {
			case !other.Known:
				maybe = append(maybe, fmt.Sprintf("whether %s uses it, since %s", other.Identity.Description(), other.Unknown))
			case slices.Contains(other.Names, name):
				users = append(users, other.Identity)
			}
		}

		switch {
		case len(users) > 0:
			plan.Notices = append(plan.Notices, fmt.Sprintf("Kept CA key pair %q in the local secrets cache: still used by %s.",
				name, localclusters.DescribeClusters(users)))
		case len(maybe) > 0:
			plan.Notices = append(plan.Notices, fmt.Sprintf("Kept CA key pair %q in the local secrets cache: can't tell %s.",
				name, strings.Join(maybe, "; nor ")))
		default:
			plan.Remove = append(plan.Remove, name)
		}
	}

	return plan
}

// isCAKeyPairName reports whether name is a CA key pair's name.
func isCAKeyPairName(name string) bool {
	role, ok := secrets.ClassifyCachedSecretName(name)
	return ok && role == secrets.CAKeyPairRole
}
