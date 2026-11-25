package v1beta1

// ReadyCondition is the type of the condition each of the four kinds
// reports: whether the resource is in sync, and why not.
const ReadyCondition = "Ready"

// The reasons of the Ready condition.
const (
	// ReasonInSync means every target a SecretCopier, SecretExporter or
	// SecretInjector matches is in sync.
	ReasonInSync = "InSync"

	// ReasonInvalidSelector means a rule's selectors are invalid, so the
	// rule selects nothing.
	ReasonInvalidSelector = "InvalidSelector"

	// ReasonSourceMissing means a source Secret does not exist, or is in a
	// namespace being deleted, so it is not copied.
	ReasonSourceMissing = "SourceMissing"

	// ReasonTargetErrors means some targets failed, and are retried.
	ReasonTargetErrors = "TargetErrors"

	// ReasonConflict means a Secret the secrets-manager did not create, or
	// one another rule manages from another source, holds a copy's name in
	// a target namespace. It is left untouched.
	ReasonConflict = "Conflict"

	// ReasonAwaitingAuthorization means a target namespace does not accept
	// a copy: no SecretImporter accepts it there. For a SecretImporter, it
	// means the importer accepts none of the offers of its Secret.
	ReasonAwaitingAuthorization = "AwaitingAuthorization"

	// ReasonImported means a SecretImporter's Secret has been copied into
	// its namespace by an exporter or copier whose offer it accepts.
	ReasonImported = "Imported"

	// ReasonAwaitingCopy means a SecretImporter accepts an offer of its
	// Secret, and the copy is not there yet: the offering resource has not
	// written it, or cannot, as its status says.
	ReasonAwaitingCopy = "AwaitingCopy"

	// ReasonNoMatchingExporter means no SecretExporter or SecretCopier rule
	// offers a SecretImporter's Secret to its namespace.
	ReasonNoMatchingExporter = "NoMatchingExporter"
)

// TargetCounts count the targets of a SecretCopier or SecretExporter, or of
// one of its rules: the namespaces matched, and what holds in them.
type TargetCounts struct {
	// targetNamespaces counts the target namespaces matched.
	TargetNamespaces int32 `json:"targetNamespaces"`

	// inSync counts the targets whose copy is in sync.
	InSync int32 `json:"inSync"`

	// conflicts counts the targets holding a Secret of the copy's name that
	// the secrets-manager did not create, or that another rule manages from
	// another source.
	Conflicts int32 `json:"conflicts"`

	// awaitingAuthorization counts the targets where no SecretImporter
	// accepts the copy.
	AwaitingAuthorization int32 `json:"awaitingAuthorization"`

	// failures counts the targets that failed, and are retried.
	Failures int32 `json:"failures"`
}

// CopySummary counts the targets of every rule of a SecretCopier or
// SecretExporter.
type CopySummary struct {
	TargetCounts `json:",inline"`

	// inSyncRatio is inSync out of targetNamespaces, as kubectl get shows
	// it, such as 5/6.
	InSyncRatio string `json:"inSyncRatio"`
}
