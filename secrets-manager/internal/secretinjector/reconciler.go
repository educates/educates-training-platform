// Package secretinjector reconciles SecretInjectors: it adds the Secrets
// each rule selects to the service accounts the rule selects, in the
// namespaces it selects. Image pull Secrets go into a service account's
// imagePullSecrets and other Secrets into its secrets. It only ever adds
// names: a name stays when its rule, its injector or its Secret goes.
package secretinjector

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/convergence"
	"github.com/educates/educates-training-platform/secrets-manager/internal/eventmap"
	"github.com/educates/educates-training-platform/secrets-manager/internal/selectors"
)

// conflictRetryDelay is how long a SecretInjector waits to be reconciled
// again after a service account changed while it was written, time for the
// cache to catch up with the change.
const conflictRetryDelay = time.Second

// Reconciler reconciles one SecretInjector at a time.
type Reconciler struct {
	cache       cache.Cache
	live        client.Reader
	writer      client.Writer
	types       *secretTypes
	convergence convergence.Policy
}

// New returns a reconciler that reads SecretInjectors, namespaces, service
// accounts and the metadata of Secrets from objects, the manager's cache,
// reads the type of a Secret it has not seen from live, the API server,
// writes service accounts with writer, and retries under policy.
func New(objects cache.Cache, live client.Reader, writer client.Writer, policy convergence.Policy) *Reconciler {
	return &Reconciler{cache: objects, live: live, writer: writer, types: newSecretTypes(), convergence: policy}
}

// SetupWithManager registers the reconciler with mgr. It reconciles a
// SecretInjector when its spec changes; when a Secret it may select is
// created, relabelled or given other owners; when a service account it may
// select is created, relabelled or loses a name; and when a namespace it
// may select is created, relabelled or given other owners. Besides, the
// reconciler's policy reconciles each SecretInjector again a backstop
// interval after its last reconcile, and retries a failed one with backoff.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return r.convergence.Complete(ctrl.NewControllerManagedBy(mgr).
		Named("secretinjector").
		For(&secretsv1beta1.SecretInjector{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(eventmap.SecretInjectorsForSecret(r.cache)),
			builder.OnlyMetadata, builder.WithPredicates(eventmap.SecretSelectionChanged())).
		WatchesRawSource(source.Kind(r.cache, &corev1.ServiceAccount{},
			handler.TypedEnqueueRequestsFromMapFunc(eventmap.SecretInjectorsForServiceAccount(r.cache)),
			eventmap.ServiceAccountInjectionChanged())).
		WatchesRawSource(source.Kind(r.cache, &corev1.Namespace{},
			handler.TypedEnqueueRequestsFromMapFunc(eventmap.SecretInjectorsForNamespace(r.cache)),
			eventmap.NamespaceSelectionChanged())),
		&secretsv1beta1.SecretInjector{}, r.cache, r)
}

// Reconcile injects the Secrets of each rule of the SecretInjector req
// names. A failure on one service account is reported in the returned
// error and never stops the others.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	injector := &secretsv1beta1.SecretInjector{}
	if err := r.cache.Get(ctx, req.NamespacedName, injector); err != nil {
		if apierrors.IsNotFound(err) {
			r.types.forget(req.Name)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !injector.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	outcome, err := r.Inject(ctx, injector)
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := outcome.Err(); err != nil {
		return ctrl.Result{}, err
	}
	if outcome.Conflicted {
		return ctrl.Result{RequeueAfter: conflictRetryDelay}, nil
	}
	return ctrl.Result{}, nil
}

// Inject adds the Secrets each rule of injector selects to the service
// accounts the rule selects, writing each service account at most once,
// with every name it lacks, and reports what each rule found and did. It
// fails only when it cannot list the namespaces.
func (r *Reconciler) Inject(ctx context.Context, injector *secretsv1beta1.SecretInjector) (Outcome, error) {
	namespaces := &corev1.NamespaceList{}
	if err := r.cache.List(ctx, namespaces); err != nil {
		return Outcome{}, fmt.Errorf("listing namespaces: %w", err)
	}

	p := &pass{
		reconciler: r,
		known:      r.types.remembered(injector.Name),
		seen:       map[types.UID]corev1.SecretType{},
		unreadable: map[types.UID]error{},
		contents:   map[string]*namespaceContents{},
		accounts:   map[types.NamespacedName]*pendingAccount{},
	}
	plans := make([]rulePlan, len(injector.Spec.Rules))
	for i := range injector.Spec.Rules {
		plans[i] = p.planRule(ctx, &injector.Spec.Rules[i], namespaces.Items)
	}
	p.write(ctx)
	r.types.remember(injector.Name, p.seen)

	outcome := Outcome{Rules: make([]RuleOutcome, len(plans))}
	for i := range plans {
		outcome.Rules[i] = p.tally(&plans[i], &outcome.Conflicted)
	}
	return outcome, nil
}

// pass is the state of one reconcile of a SecretInjector.
type pass struct {
	reconciler *Reconciler

	// known are the types of Secrets an earlier reconcile read, by UID, and
	// seen those this one used. A Secret's type never changes.
	known, seen map[types.UID]corev1.SecretType
	// unreadable are the Secrets whose type could not be read, and why.
	unreadable map[types.UID]error

	// contents are the Secrets and service accounts of the namespaces the
	// pass looked in, by namespace.
	contents map[string]*namespaceContents
	// accounts are the service accounts the rules select, with the names
	// they lack.
	accounts map[types.NamespacedName]*pendingAccount
}

// namespaceContents are the Secrets, as metadata, and the service accounts
// of a namespace, sorted by name, or why they could not be listed.
type namespaceContents struct {
	secrets  []metav1.PartialObjectMetadata
	accounts []corev1.ServiceAccount
	err      error
}

// pendingAccount is a service account a rule selects, with the names the
// rules add to it.
type pendingAccount struct {
	// account is the service account as the cache holds it.
	account *corev1.ServiceAccount
	// imagePullSecrets and secrets are the names to add to those lists, in
	// the order the rules add them.
	imagePullSecrets, secrets []string
	// written reports a successful write, gone that the service account no
	// longer exists, conflicted that it changed while it was written, and
	// err why it could not be written otherwise.
	written, gone, conflicted bool
	err                       error
}

// rulePlan is what a rule selects: its namespaces and, in each, the service
// accounts it injects into.
type rulePlan struct {
	invalid          error
	targetNamespaces int
	targets          []ruleTarget
	// failures are the namespaces whose contents could not be listed.
	failures []Failure
}

// ruleTarget is a service account a rule selects, with how many of the
// names the rule injects into it the service account holds, how many it
// lacks, and why the rule cannot inject into it.
type ruleTarget struct {
	account          types.NamespacedName
	present, missing int
	err              error
}

// planRule finds the service accounts rule injects into among namespaces,
// and adds the names they lack to the pass's pending accounts.
func (p *pass) planRule(ctx context.Context, rule *secretsv1beta1.SecretInjectorRule,
	namespaces []corev1.Namespace) rulePlan {
	namespaceSelector := selectors.Namespaces(rule.TargetNamespaces)
	secretSelector := selectors.SourceSecrets(rule.SourceSecrets)
	accountSelector := selectors.ServiceAccounts(rule.ServiceAccounts)
	for _, err := range []error{namespaceSelector.Err(), secretSelector.Err(), accountSelector.Err()} {
		if err != nil {
			ctrl.LoggerFrom(ctx).Info("The rule's selectors are invalid: the rule selects nothing", "reason", err.Error())
			return rulePlan{invalid: err}
		}
	}

	var plan rulePlan
	for i := range namespaces {
		ns := &namespaces[i]
		if !namespaceSelector.Match(ns) {
			continue
		}
		plan.targetNamespaces++
		contents := p.namespaceContents(ctx, ns.Name)
		if contents.err != nil {
			plan.failures = append(plan.failures, Failure{Namespace: ns.Name, Err: contents.err})
			continue
		}

		var accounts []*corev1.ServiceAccount
		for j := range contents.accounts {
			account := &contents.accounts[j]
			if account.DeletionTimestamp.IsZero() && accountSelector.Match(account) {
				accounts = append(accounts, account)
			}
		}
		if len(accounts) == 0 {
			continue
		}
		injections, err := p.injections(ctx, contents, secretSelector)
		for _, account := range accounts {
			plan.targets = append(plan.targets, p.target(account, injections, err))
		}
	}
	return plan
}

// injection is a name a rule adds to a service account, and whether it goes
// into imagePullSecrets.
type injection struct {
	name string
	pull bool
}

// injections lists the Secrets of a namespace that selector selects, as the
// names to inject, and joins the errors reading their types gave.
func (p *pass) injections(ctx context.Context, contents *namespaceContents,
	selector selectors.Selector) ([]injection, error) {
	var result []injection
	var errs []error
	for i := range contents.secrets {
		secret := &contents.secrets[i]
		if !selector.Match(secret) {
			continue
		}
		secretType, found, err := p.secretType(ctx, secret)
		switch {
		case err != nil:
			errs = append(errs, err)
		case found:
			result = append(result, injection{name: secret.Name, pull: isImagePullSecret(secretType)})
		}
	}
	return result, errors.Join(errs...)
}

// target adds the names of injections that account lacks to its pending
// account, and returns the account as a target of the rule. err is why the
// rule cannot inject some of its Secrets.
func (p *pass) target(account *corev1.ServiceAccount, injections []injection, err error) ruleTarget {
	key := client.ObjectKeyFromObject(account)
	pending, ok := p.accounts[key]
	if !ok {
		pending = &pendingAccount{account: account}
		p.accounts[key] = pending
	}
	target := ruleTarget{account: key, err: err}
	for _, inj := range injections {
		if pending.holds(inj) {
			target.present++
			continue
		}
		target.missing++
		pending.add(inj)
	}
	return target
}

// holds reports whether the service account, as the cache holds it, names
// the Secret of inj in the list inj goes into. Entries are compared by
// name only, whatever other fields they carry.
func (a *pendingAccount) holds(inj injection) bool {
	if inj.pull {
		return slices.ContainsFunc(a.account.ImagePullSecrets,
			func(ref corev1.LocalObjectReference) bool { return ref.Name == inj.name })
	}
	return slices.ContainsFunc(a.account.Secrets, func(ref corev1.ObjectReference) bool { return ref.Name == inj.name })
}

// add records the name of inj as one to add to the service account, once.
func (a *pendingAccount) add(inj injection) {
	names := &a.secrets
	if inj.pull {
		names = &a.imagePullSecrets
	}
	if !slices.Contains(*names, inj.name) {
		*names = append(*names, inj.name)
	}
}

// namespaceContents lists the Secrets and service accounts of namespace ns
// from the cache, once per pass.
func (p *pass) namespaceContents(ctx context.Context, ns string) *namespaceContents {
	if contents, ok := p.contents[ns]; ok {
		return contents
	}
	contents := &namespaceContents{}
	p.contents[ns] = contents

	secrets := &metav1.PartialObjectMetadataList{}
	secrets.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("SecretList"))
	if err := p.reconciler.cache.List(ctx, secrets, client.InNamespace(ns)); err != nil {
		contents.err = fmt.Errorf("listing the Secrets of namespace %s: %w", ns, err)
		return contents
	}
	accounts := &corev1.ServiceAccountList{}
	if err := p.reconciler.cache.List(ctx, accounts, client.InNamespace(ns)); err != nil {
		contents.err = fmt.Errorf("listing the service accounts of namespace %s: %w", ns, err)
		return contents
	}
	contents.secrets = secrets.Items
	slices.SortFunc(contents.secrets, func(a, b metav1.PartialObjectMetadata) int { return strings.Compare(a.Name, b.Name) })
	contents.accounts = accounts.Items
	slices.SortFunc(contents.accounts, func(a, b corev1.ServiceAccount) int { return strings.Compare(a.Name, b.Name) })
	return contents
}

// secretType returns the type of secret, read from the API server unless a
// reconcile has read it before, and whether the Secret the cache holds
// still exists.
func (p *pass) secretType(ctx context.Context, secret *metav1.PartialObjectMetadata) (corev1.SecretType, bool, error) {
	if secretType, ok := p.seen[secret.UID]; ok {
		return secretType, true, nil
	}
	if err, ok := p.unreadable[secret.UID]; ok {
		return "", false, err
	}
	if secretType, ok := p.known[secret.UID]; ok {
		p.seen[secret.UID] = secretType
		return secretType, true, nil
	}

	live := &corev1.Secret{}
	err := p.reconciler.live.Get(ctx, client.ObjectKeyFromObject(secret), live)
	switch {
	case apierrors.IsNotFound(err):
		return "", false, nil
	case err != nil:
		err = fmt.Errorf("reading the type of Secret %s/%s: %w", secret.Namespace, secret.Name, err)
		p.unreadable[secret.UID] = err
		return "", false, err
	case live.UID != secret.UID:
		// The Secret was recreated since the cache saw it: its creation
		// brings the injector back once the cache holds it.
		return "", false, nil
	}
	p.seen[secret.UID] = live.Type
	return live.Type, true, nil
}

// write adds to each pending service account the names it lacks, in one
// write with optimistic concurrency.
func (p *pass) write(ctx context.Context) {
	keys := make([]types.NamespacedName, 0, len(p.accounts))
	for key, pending := range p.accounts {
		if len(pending.imagePullSecrets) > 0 || len(pending.secrets) > 0 {
			keys = append(keys, key)
		}
	}
	slices.SortFunc(keys, func(a, b types.NamespacedName) int { return strings.Compare(a.String(), b.String()) })
	for _, key := range keys {
		p.accounts[key].write(ctx, p.reconciler.writer)
	}
}

// write adds the pending names to the service account, conditional on the
// resourceVersion the cache holds.
func (a *pendingAccount) write(ctx context.Context, writer client.Writer) {
	updated := a.account.DeepCopy()
	for _, name := range a.imagePullSecrets {
		updated.ImagePullSecrets = append(updated.ImagePullSecrets, corev1.LocalObjectReference{Name: name})
	}
	for _, name := range a.secrets {
		updated.Secrets = append(updated.Secrets, corev1.ObjectReference{Name: name})
	}
	log := ctrl.LoggerFrom(ctx).WithValues("namespace", a.account.Namespace, "serviceAccount", a.account.Name)
	err := writer.Patch(ctx, updated, client.MergeFromWithOptions(a.account, client.MergeFromWithOptimisticLock{}))
	switch {
	case err == nil:
		a.written = true
		convergence.Repaired(ctx)
		log.Info("Injected Secrets into the service account",
			"imagePullSecrets", a.imagePullSecrets, "secrets", a.secrets)
	case apierrors.IsNotFound(err):
		a.gone = true
	case apierrors.IsConflict(err):
		a.conflicted = true
		log.V(1).Info("The service account changed while it was written: retrying")
	default:
		a.err = fmt.Errorf("injecting Secrets into service account %s/%s: %w", a.account.Namespace, a.account.Name, err)
	}
}

// tally counts what plan found and did once the pending accounts are
// written, and sets conflicted when one of its service accounts changed
// while it was written.
func (p *pass) tally(plan *rulePlan, conflicted *bool) RuleOutcome {
	outcome := RuleOutcome{
		InvalidSelector:  plan.invalid,
		TargetNamespaces: plan.targetNamespaces,
		Failures:         slices.Clone(plan.failures),
	}
	for _, target := range plan.targets {
		pending := p.accounts[target.account]
		if pending.gone {
			continue
		}
		outcome.ServiceAccountsMatched++
		inSync := target.missing == 0 || pending.written
		if inSync {
			outcome.InjectionsInSync += target.present + target.missing
		} else {
			outcome.InjectionsInSync += target.present
		}

		err := target.err
		if err == nil && !inSync {
			err = pending.err
		}
		switch {
		case err != nil:
			outcome.Failures = append(outcome.Failures, Failure{
				Namespace: target.account.Namespace, ServiceAccount: target.account.Name, Err: err,
			})
		case inSync:
			outcome.ServiceAccountsInSync++
		case pending.conflicted:
			*conflicted = true
		}
	}
	return outcome
}

// isImagePullSecret reports whether a Secret of secretType holds image pull
// credentials, which go into a service account's imagePullSecrets.
func isImagePullSecret(secretType corev1.SecretType) bool {
	return secretType == corev1.SecretTypeDockerConfigJson || secretType == corev1.SecretTypeDockercfg
}
