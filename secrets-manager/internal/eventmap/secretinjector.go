package eventmap

import (
	"context"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/selectors"
)

// SecretSelectionChanged passes the events on a Secret, of which it needs
// only the metadata, that can make a SecretInjector rule select it: its
// creation, and a change of its labels or owners. An injector never
// removes a name, so a deleted Secret needs no reconcile.
func SecretSelectionChanged() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(event.CreateEvent) bool { return true },
		UpdateFunc: func(e event.UpdateEvent) bool {
			return !maps.Equal(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) ||
				!equality.Semantic.DeepEqual(e.ObjectOld.GetOwnerReferences(), e.ObjectNew.GetOwnerReferences())
		},
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

// ServiceAccountInjectionChanged passes the events on a service account
// that can leave it lacking a name a SecretInjector rule injects: its
// creation, a change of its labels, and a change of its secrets or
// imagePullSecrets, such as a name removed by hand.
func ServiceAccountInjectionChanged() predicate.TypedPredicate[*corev1.ServiceAccount] {
	return predicate.TypedFuncs[*corev1.ServiceAccount]{
		CreateFunc: func(event.TypedCreateEvent[*corev1.ServiceAccount]) bool { return true },
		UpdateFunc: func(e event.TypedUpdateEvent[*corev1.ServiceAccount]) bool {
			return !maps.Equal(e.ObjectOld.Labels, e.ObjectNew.Labels) ||
				!equality.Semantic.DeepEqual(e.ObjectOld.Secrets, e.ObjectNew.Secrets) ||
				!equality.Semantic.DeepEqual(e.ObjectOld.ImagePullSecrets, e.ObjectNew.ImagePullSecrets)
		},
		DeleteFunc:  func(event.TypedDeleteEvent[*corev1.ServiceAccount]) bool { return false },
		GenericFunc: func(event.TypedGenericEvent[*corev1.ServiceAccount]) bool { return false },
	}
}

// SecretInjectorsForSecret maps an event on a Secret, of which it needs
// only the metadata, to the SecretInjectors with a rule that selects it in
// its namespace. It reads the SecretInjectors and the namespace from
// reader.
func SecretInjectorsForSecret(reader client.Reader) handler.MapFunc {
	return func(ctx context.Context, secret client.Object) []reconcile.Request {
		return secretInjectorsFor(ctx, reader, secret, func(rule *secretsv1beta1.SecretInjectorRule) bool {
			return selectors.SourceSecrets(rule.SourceSecrets).Match(secret)
		})
	}
}

// SecretInjectorsForServiceAccount maps an event on a service account to
// the SecretInjectors with a rule that selects it in its namespace. It
// reads the SecretInjectors and the namespace from reader.
func SecretInjectorsForServiceAccount(
	reader client.Reader) handler.TypedMapFunc[*corev1.ServiceAccount, reconcile.Request] {
	return func(ctx context.Context, account *corev1.ServiceAccount) []reconcile.Request {
		return secretInjectorsFor(ctx, reader, account, func(rule *secretsv1beta1.SecretInjectorRule) bool {
			return selectors.ServiceAccounts(rule.ServiceAccounts).Match(account)
		})
	}
}

// SecretInjectorsForNamespace maps an event on a namespace to the
// SecretInjectors with a rule that targets it, evaluating the selectors of
// the SecretInjectors reader holds.
func SecretInjectorsForNamespace(reader client.Reader) handler.TypedMapFunc[*corev1.Namespace, reconcile.Request] {
	return func(ctx context.Context, ns *corev1.Namespace) []reconcile.Request {
		injectors := &secretsv1beta1.SecretInjectorList{}
		if err := reader.List(ctx, injectors); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "Listing SecretInjectors for a namespace failed", "namespace", ns.Name)
			return nil
		}
		names := map[string]struct{}{}
		for _, injector := range injectors.Items {
			if slices.ContainsFunc(injector.Spec.Rules, func(rule secretsv1beta1.SecretInjectorRule) bool {
				return selectors.Namespaces(rule.TargetNamespaces).Match(ns)
			}) {
				names[injector.Name] = struct{}{}
			}
		}
		return requests(names)
	}
}

// secretInjectorsFor returns a reconcile request for each SecretInjector
// with a rule that targets the namespace of obj and for which selects
// reports true. When the namespace cannot be read, the namespace selectors
// are not evaluated.
func secretInjectorsFor(ctx context.Context, reader client.Reader, obj metav1.Object,
	selects func(*secretsv1beta1.SecretInjectorRule) bool) []reconcile.Request {
	log := ctrl.LoggerFrom(ctx).WithValues("namespace", obj.GetNamespace(), "name", obj.GetName())
	injectors := &secretsv1beta1.SecretInjectorList{}
	if err := reader.List(ctx, injectors); err != nil {
		log.Error(err, "Listing SecretInjectors failed")
		return nil
	}
	if len(injectors.Items) == 0 {
		return nil
	}

	ns := &corev1.Namespace{}
	err := reader.Get(ctx, types.NamespacedName{Name: obj.GetNamespace()}, ns)
	if apierrors.IsNotFound(err) {
		// The namespace's creation maps it once the cache holds it.
		return nil
	}
	if err != nil {
		log.Error(err, "Reading the namespace failed: mapping without the namespace selectors")
		ns = nil
	}

	names := map[string]struct{}{}
	for _, injector := range injectors.Items {
		for i := range injector.Spec.Rules {
			rule := &injector.Spec.Rules[i]
			if (ns == nil || selectors.Namespaces(rule.TargetNamespaces).Match(ns)) && selects(rule) {
				names[injector.Name] = struct{}{}
				break
			}
		}
	}
	return requests(names)
}
