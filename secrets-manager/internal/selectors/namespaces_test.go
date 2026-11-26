package selectors

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
)

// activeNamespace is the Active namespace name with labels.
func activeNamespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
	}
}

// owner is an owner a rule selects namespaces by.
var owner = secretsv1beta1.OwnerReference{APIVersion: "v1", Kind: "Namespace", Name: "owner", UID: "owner-uid"}

// terminating marks ns Terminating.
func terminating(ns *corev1.Namespace) *corev1.Namespace {
	ns.Status.Phase = corev1.NamespaceTerminating
	return ns
}

// withUID gives ns the UID uid.
func withUID(ns *corev1.Namespace, uid types.UID) *corev1.Namespace {
	ns.UID = uid
	return ns
}

// ownedBy gives ns the owner references owners.
func ownedBy(ns *corev1.Namespace, owners ...metav1.OwnerReference) *corev1.Namespace {
	ns.OwnerReferences = owners
	return ns
}

// requirement is a matchExpressions entry.
func requirement(key string, operator metav1.LabelSelectorOperator, values ...string) metav1.LabelSelectorRequirement {
	return metav1.LabelSelectorRequirement{Key: key, Operator: operator, Values: values}
}

func TestNamespacesMatch(t *testing.T) {
	names := func(patterns ...string) secretsv1beta1.TargetNamespaces {
		return secretsv1beta1.TargetNamespaces{NameSelector: &secretsv1beta1.NameSelector{MatchNames: patterns}}
	}
	matchLabels := func(labels map[string]string) secretsv1beta1.TargetNamespaces {
		return secretsv1beta1.TargetNamespaces{LabelSelector: &metav1.LabelSelector{MatchLabels: labels}}
	}
	expressions := func(requirements ...metav1.LabelSelectorRequirement) secretsv1beta1.TargetNamespaces {
		return secretsv1beta1.TargetNamespaces{LabelSelector: &metav1.LabelSelector{MatchExpressions: requirements}}
	}
	uids := func(uids ...string) secretsv1beta1.TargetNamespaces {
		return secretsv1beta1.TargetNamespaces{UIDSelector: &secretsv1beta1.UIDSelector{MatchUIDs: uids}}
	}
	owners := func(owners ...secretsv1beta1.OwnerReference) secretsv1beta1.TargetNamespaces {
		return secretsv1beta1.TargetNamespaces{OwnerSelector: &secretsv1beta1.OwnerSelector{MatchOwners: owners}}
	}
	gold := map[string]string{"tier": "gold"}
	silver := map[string]string{"tier": "silver"}
	portal := secretsv1beta1.OwnerReference{
		APIVersion: "training.educates.dev/v1beta1", Kind: "TrainingPortal", Name: "portal", UID: "portal-uid"}
	portalOwner := metav1.OwnerReference{
		APIVersion: portal.APIVersion, Kind: portal.Kind, Name: portal.Name, UID: portal.UID, Controller: new(true)}
	otherOwner := metav1.OwnerReference{APIVersion: "v1", Kind: "Namespace", Name: "other", UID: "other-uid"}
	tests := []struct {
		name      string
		selector  secretsv1beta1.TargetNamespaces
		namespace *corev1.Namespace
		want      bool
	}{
		{"without a name selector, any namespace", secretsv1beta1.TargetNamespaces{},
			activeNamespace("apps", nil), true},
		{"without a name selector, no kube- namespace", secretsv1beta1.TargetNamespaces{},
			activeNamespace("kube-system", nil), false},
		{"with an empty matchNames, any namespace", names(), activeNamespace("apps", nil), true},
		{"with an empty matchNames, no kube- namespace", names(), activeNamespace("kube-system", nil), false},
		{"a listed name", names("apps", "web"), activeNamespace("web", nil), true},
		{"a name not listed", names("apps", "web"), activeNamespace("db", nil), false},
		{"a glob", names("team-*"), activeNamespace("team-a", nil), true},
		{"an exclusion", names("team-*", "!team-b"), activeNamespace("team-b", nil), false},
		{"listing a kube- namespace selects it", names("kube-system"), activeNamespace("kube-system", nil), true},

		{"matchLabels, with every label", matchLabels(gold),
			activeNamespace("apps", map[string]string{"tier": "gold", "team": "a"}), true},
		{"matchLabels, with another value", matchLabels(gold),
			activeNamespace("apps", map[string]string{"tier": "silver"}), false},
		{"matchLabels, without the label", matchLabels(gold), activeNamespace("apps", nil), false},
		{"matchLabels, still no kube- namespace", matchLabels(gold), activeNamespace("kube-system", gold), false},
		{"an empty label selector, any namespace", matchLabels(nil), activeNamespace("apps", nil), true},

		{"In, with a listed value", expressions(requirement("tier", metav1.LabelSelectorOpIn, "gold", "bronze")),
			activeNamespace("apps", gold), true},
		{"In, with another value", expressions(requirement("tier", metav1.LabelSelectorOpIn, "gold")),
			activeNamespace("apps", silver), false},
		{"In, without the label", expressions(requirement("tier", metav1.LabelSelectorOpIn, "gold")),
			activeNamespace("apps", nil), false},
		{"NotIn, with a listed value", expressions(requirement("tier", metav1.LabelSelectorOpNotIn, "gold")),
			activeNamespace("apps", gold), false},
		{"NotIn, with another value", expressions(requirement("tier", metav1.LabelSelectorOpNotIn, "gold")),
			activeNamespace("apps", silver), true},
		{"NotIn, without the label", expressions(requirement("tier", metav1.LabelSelectorOpNotIn, "gold")),
			activeNamespace("apps", nil), true},
		{"Exists, with the label", expressions(requirement("tier", metav1.LabelSelectorOpExists)),
			activeNamespace("apps", silver), true},
		{"Exists, without the label", expressions(requirement("tier", metav1.LabelSelectorOpExists)),
			activeNamespace("apps", nil), false},
		{"DoesNotExist, with the label", expressions(requirement("tier", metav1.LabelSelectorOpDoesNotExist)),
			activeNamespace("apps", gold), false},
		{"DoesNotExist, without the label", expressions(requirement("tier", metav1.LabelSelectorOpDoesNotExist)),
			activeNamespace("apps", nil), true},
		{"every expression must pass", expressions(
			requirement("tier", metav1.LabelSelectorOpExists), requirement("team", metav1.LabelSelectorOpIn, "a")),
			activeNamespace("apps", gold), false},
		{"matchLabels and matchExpressions must both pass", secretsv1beta1.TargetNamespaces{
			LabelSelector: &metav1.LabelSelector{
				MatchLabels:      gold,
				MatchExpressions: []metav1.LabelSelectorRequirement{requirement("team", metav1.LabelSelectorOpExists)},
			}}, activeNamespace("apps", gold), false},
		{"In without values matches nothing", expressions(requirement("tier", metav1.LabelSelectorOpIn)),
			activeNamespace("apps", gold), false},
		{"NotIn without values matches nothing", expressions(requirement("tier", metav1.LabelSelectorOpNotIn)),
			activeNamespace("apps", nil), false},

		{"a listed UID", uids("uid-a", "uid-b"), withUID(activeNamespace("apps", nil), "uid-b"), true},
		{"a UID not listed", uids("uid-a"), withUID(activeNamespace("apps", nil), "uid-b"), false},
		{"an empty matchUIDs, any UID", uids(), withUID(activeNamespace("apps", nil), "uid-b"), true},
		{"a UID still no kube- namespace", uids("uid-a"), withUID(activeNamespace("kube-system", nil), "uid-a"), false},

		{"an owner reference that is listed", owners(portal),
			ownedBy(activeNamespace("apps", nil), otherOwner, portalOwner), true},
		{"without owner references", owners(portal), activeNamespace("apps", nil), false},
		{"an owner reference not listed", owners(portal), ownedBy(activeNamespace("apps", nil), otherOwner), false},
		{"an owner with another apiVersion", owners(portal), ownedBy(activeNamespace("apps", nil),
			metav1.OwnerReference{APIVersion: "v1", Kind: portal.Kind, Name: portal.Name, UID: portal.UID}), false},
		{"an owner with another kind", owners(portal), ownedBy(activeNamespace("apps", nil),
			metav1.OwnerReference{APIVersion: portal.APIVersion, Kind: "Workshop", Name: portal.Name, UID: portal.UID}),
			false},
		{"an owner with another name", owners(portal), ownedBy(activeNamespace("apps", nil),
			metav1.OwnerReference{APIVersion: portal.APIVersion, Kind: portal.Kind, Name: "other", UID: portal.UID}),
			false},
		{"an owner with another UID", owners(portal), ownedBy(activeNamespace("apps", nil),
			metav1.OwnerReference{APIVersion: portal.APIVersion, Kind: portal.Kind, Name: portal.Name, UID: "other"}),
			false},
		{"an empty matchOwners, any namespace", owners(), activeNamespace("apps", nil), true},

		{"a terminating namespace", secretsv1beta1.TargetNamespaces{}, terminating(activeNamespace("apps", nil)), false},
		{"a terminating namespace, though listed", names("apps"), terminating(activeNamespace("apps", nil)), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Namespaces(test.selector).Match(test.namespace); got != test.want {
				t.Errorf("Namespaces(%+v).Match(%q) = %v, want %v", test.selector, test.namespace.Name, got, test.want)
			}
		})
	}
}

func TestNamespacesMatchEverySelector(t *testing.T) {
	selector := secretsv1beta1.TargetNamespaces{
		NameSelector:  &secretsv1beta1.NameSelector{MatchNames: []string{"team-*"}},
		UIDSelector:   &secretsv1beta1.UIDSelector{MatchUIDs: []string{"uid-a"}},
		OwnerSelector: &secretsv1beta1.OwnerSelector{MatchOwners: []secretsv1beta1.OwnerReference{owner}},
		LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "gold"}},
	}
	tests := []struct {
		name   string
		adjust func(*corev1.Namespace)
		want   bool
	}{
		{"passing every selector", func(*corev1.Namespace) {}, true},
		{"failing the name selector", func(ns *corev1.Namespace) { ns.Name = "apps" }, false},
		{"failing the UID selector", func(ns *corev1.Namespace) { ns.UID = "uid-b" }, false},
		{"failing the owner selector", func(ns *corev1.Namespace) { ns.OwnerReferences = nil }, false},
		{"failing the label selector", func(ns *corev1.Namespace) { ns.Labels = nil }, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ns := ownedBy(withUID(activeNamespace("team-a", map[string]string{"tier": "gold"}), "uid-a"),
				metav1.OwnerReference{APIVersion: owner.APIVersion, Kind: owner.Kind, Name: owner.Name, UID: owner.UID})
			test.adjust(ns)
			if got := Namespaces(selector).Match(ns); got != test.want {
				t.Errorf("Namespaces(%+v).Match(%+v) = %v, want %v", selector, ns.ObjectMeta, got, test.want)
			}
		})
	}
}

func TestNamespacesErr(t *testing.T) {
	expressions := func(requirements ...metav1.LabelSelectorRequirement) secretsv1beta1.TargetNamespaces {
		return secretsv1beta1.TargetNamespaces{LabelSelector: &metav1.LabelSelector{MatchExpressions: requirements}}
	}
	tests := []struct {
		name     string
		selector secretsv1beta1.TargetNamespaces
		invalid  bool
	}{
		{"no selector", secretsv1beta1.TargetNamespaces{}, false},
		{"In with values", expressions(requirement("tier", metav1.LabelSelectorOpIn, "gold")), false},
		{"NotIn with values", expressions(requirement("tier", metav1.LabelSelectorOpNotIn, "gold")), false},
		{"Exists", expressions(requirement("tier", metav1.LabelSelectorOpExists)), false},
		{"DoesNotExist", expressions(requirement("tier", metav1.LabelSelectorOpDoesNotExist)), false},
		{"In without values", expressions(requirement("tier", metav1.LabelSelectorOpIn)), true},
		{"NotIn without values", expressions(requirement("tier", metav1.LabelSelectorOpNotIn)), true},
		{"a valid expression beside one without values", expressions(
			requirement("tier", metav1.LabelSelectorOpExists), requirement("team", metav1.LabelSelectorOpIn)), true},
		{"Exists with values", expressions(requirement("tier", metav1.LabelSelectorOpExists, "gold")), true},
		{"DoesNotExist with values", expressions(requirement("tier", metav1.LabelSelectorOpDoesNotExist, "gold")), true},
		{"an unknown operator", expressions(requirement("tier", metav1.LabelSelectorOperator("Near"), "gold")), true},
		{"an invalid key in an expression", expressions(requirement("not a key", metav1.LabelSelectorOpExists)), true},
		{"an invalid value in an expression", expressions(requirement("tier", metav1.LabelSelectorOpIn, "not a value")), true},
		{"valid matchLabels", secretsv1beta1.TargetNamespaces{
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "gold"}}}, false},
		{"an invalid key in matchLabels", secretsv1beta1.TargetNamespaces{
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"not a key": "gold"}}}, true},
		{"an invalid value in matchLabels", secretsv1beta1.TargetNamespaces{
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"tier": "not a value"}}}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := Namespaces(test.selector).Err()
			if invalid := err != nil; invalid != test.invalid {
				t.Errorf("Namespaces(%+v).Err() = %v, want invalid %v", test.selector, err, test.invalid)
			}
		})
	}
}
