// Package exportimport reconciles SecretExporters and SecretImporters, and
// holds the handshake between a rule offering a copy and the SecretImporter
// named after that copy in the target namespace.
//
// A SecretExporter offers the Secret of its own name in its own namespace to
// the namespaces its rules select. A copy is made into a selected namespace
// only where a SecretImporter named after the copy accepts it: the
// importer's shared secret equals the rule's, the SecretExporter's UID when
// the rule names none, and its sourceNamespaces admit the SecretExporter's
// namespace. The copy is owned by the SecretImporter.
//
// A SecretImporter gates SecretCopier rules too: a rule with
// copyAuthorization copies only where an importer holding the same shared
// secret accepts the copy, and any importer named after a copy refuses it
// from a namespace its sourceNamespaces do not admit.
//
// A copy the importer no longer accepts, because its shared secret changed
// or it was deleted, or whose SecretExporter was deleted, stays in place and
// is no longer updated.
package exportimport

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/selectors"
)

// Offer is a rule's offer of a copy of its source into a target namespace,
// which the SecretImporter named after the copy there may refuse.
type Offer struct {
	// SourceNamespace is the namespace of the source copied.
	SourceNamespace string

	// SharedSecret is the shared secret the importer must hold, when
	// RequiresImporter is set.
	SharedSecret string

	// RequiresImporter says that the copy is made only where an importer
	// holding SharedSecret accepts it.
	RequiresImporter bool
}

// Refusal says why a target namespace does not accept an offer. It is
// empty when the target accepts it.
type Refusal string

const (
	// Accepted means the target accepts the offer.
	Accepted Refusal = ""

	// NoImporter means the offer requires an importer named after the copy
	// in the target namespace, and there is none.
	NoImporter Refusal = "NoImporter"

	// SharedSecretMismatch means the importer named after the copy holds
	// no shared secret, or another one than the offer's.
	SharedSecretMismatch Refusal = "SharedSecretMismatch"

	// SourceNamespaceRefused means the importer named after the copy does
	// not admit the source's namespace in its sourceNamespaces.
	SourceNamespaceRefused Refusal = "SourceNamespaceRefused"
)

// Authorize decides whether a target accepts offer, importer being the
// SecretImporter named after the copy in the target namespace, nil when
// there is none.
func Authorize(importer *secretsv1beta1.SecretImporter, offer Offer) Refusal {
	if importer == nil {
		if offer.RequiresImporter {
			return NoImporter
		}
		return Accepted
	}
	if offer.RequiresImporter {
		held := importer.Spec.CopyAuthorization
		if held == nil || held.SharedSecret != offer.SharedSecret {
			return SharedSecretMismatch
		}
	}
	if !admits(importer, offer.SourceNamespace) {
		return SourceNamespaceRefused
	}
	return Accepted
}

// admits reports whether the sourceNamespaces of importer admit namespace:
// every namespace when they are absent or name none.
func admits(importer *secretsv1beta1.SecretImporter, namespace string) bool {
	sources := importer.Spec.SourceNamespaces
	if sources == nil || sources.NameSelector == nil {
		return true
	}
	return selectors.MatchNames(sources.NameSelector.MatchNames, namespace)
}

// Importer reads from reader, a cache, the SecretImporter named key, the
// name of a copy and its namespace. It returns nil when there is none.
func Importer(ctx context.Context, reader client.Reader, key types.NamespacedName) (
	*secretsv1beta1.SecretImporter, error) {
	importer := &secretsv1beta1.SecretImporter{}
	err := reader.Get(ctx, key, importer)
	switch {
	case apierrors.IsNotFound(err):
		return nil, nil
	case err != nil:
		return nil, err
	case !importer.DeletionTimestamp.IsZero():
		return nil, nil
	}
	return importer, nil
}

// CopierOffer is the offer a SecretCopier rule makes of its source: one
// that requires an importer when the rule names a shared secret.
func CopierOffer(rule *secretsv1beta1.SecretCopierRule) Offer {
	offer := Offer{SourceNamespace: rule.SourceSecret.Namespace}
	if auth := rule.CopyAuthorization; auth != nil && auth.SharedSecret != "" {
		offer.SharedSecret, offer.RequiresImporter = auth.SharedSecret, true
	}
	return offer
}

// ExporterOffer is the offer a rule of exporter makes of its Secret: one
// that always requires an importer, holding the rule's shared secret, or
// the exporter's UID when the rule names none.
func ExporterOffer(exporter *secretsv1beta1.SecretExporter, rule *secretsv1beta1.SecretExporterRule) Offer {
	sharedSecret := string(exporter.UID)
	if rule.CopyAuthorization != nil {
		sharedSecret = rule.CopyAuthorization.SharedSecret
	}
	return Offer{SourceNamespace: exporter.Namespace, SharedSecret: sharedSecret, RequiresImporter: true}
}
