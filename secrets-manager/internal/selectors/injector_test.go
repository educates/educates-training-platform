package selectors

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
)

func TestSourceSecretsMatch(t *testing.T) {
	secret := func(name string, labels map[string]string) *corev1.Secret {
		return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Namespace: "apps", Name: name, Labels: labels, UID: "secret-uid",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "v1", Kind: "ConfigMap", Name: "owner", UID: "owner-uid",
			}},
		}}
	}
	names := func(patterns ...string) secretsv1beta1.SourceSecrets {
		return secretsv1beta1.SourceSecrets{NameSelector: &secretsv1beta1.NameSelector{MatchNames: patterns}}
	}
	uids := func(uids ...string) secretsv1beta1.SourceSecrets {
		return secretsv1beta1.SourceSecrets{UIDSelector: &secretsv1beta1.UIDSelector{MatchUIDs: uids}}
	}
	owners := func(owners ...secretsv1beta1.OwnerReference) secretsv1beta1.SourceSecrets {
		return secretsv1beta1.SourceSecrets{OwnerSelector: &secretsv1beta1.OwnerSelector{MatchOwners: owners}}
	}
	labelled := func(selector metav1.LabelSelector) secretsv1beta1.SourceSecrets {
		return secretsv1beta1.SourceSecrets{LabelSelector: &selector}
	}
	configMapOwner := secretsv1beta1.OwnerReference{APIVersion: "v1", Kind: "ConfigMap", Name: "owner", UID: "owner-uid"}

	tests := []struct {
		name    string
		sources secretsv1beta1.SourceSecrets
		secret  *corev1.Secret
		want    bool
	}{
		{"an exact name", names("registry-credentials"), secret("registry-credentials", nil), true},
		{"another name", names("registry-credentials"), secret("api-token", nil), false},
		{"an empty matchNames alone matches nothing", names(), secret("registry-credentials", nil), false},
		{"an empty matchNames beside a labelSelector passes any name the labels match, kube- prefixed ones included",
			secretsv1beta1.SourceSecrets{
				NameSelector:  &secretsv1beta1.NameSelector{MatchNames: []string{}},
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"inject": "yes"}},
			}, secret("kube-root-ca", map[string]string{"inject": "yes"}), true},
		{"an empty matchUIDs alone matches nothing", uids(), secret("registry-credentials", nil), false},
		{"an empty matchUIDs beside a nameSelector passes any UID the names match", secretsv1beta1.SourceSecrets{
			NameSelector: &secretsv1beta1.NameSelector{MatchNames: []string{"registry-*"}},
			UIDSelector:  &secretsv1beta1.UIDSelector{MatchUIDs: []string{}},
		}, secret("registry-credentials", nil), true},
		{"an ownerSelector without matchOwners alone matches nothing",
			secretsv1beta1.SourceSecrets{OwnerSelector: &secretsv1beta1.OwnerSelector{}},
			secret("registry-credentials", nil), false},
		{"an empty matchOwners alone matches nothing", owners(), secret("registry-credentials", nil), false},
		{"an empty matchOwners beside a nameSelector passes any owner the names match", secretsv1beta1.SourceSecrets{
			NameSelector:  &secretsv1beta1.NameSelector{MatchNames: []string{"registry-*"}},
			OwnerSelector: &secretsv1beta1.OwnerSelector{MatchOwners: []secretsv1beta1.OwnerReference{}},
		}, secret("registry-credentials", nil), true},
		{"an empty labelSelector alone matches nothing", labelled(metav1.LabelSelector{}),
			secret("registry-credentials", map[string]string{"inject": "yes"}), false},
		{"a labelSelector with empty matchLabels and matchExpressions alone matches nothing",
			labelled(metav1.LabelSelector{MatchLabels: map[string]string{},
				MatchExpressions: []metav1.LabelSelectorRequirement{}}),
			secret("registry-credentials", map[string]string{"inject": "yes"}), false},
		{"an empty labelSelector beside a nameSelector passes any labels the names match", secretsv1beta1.SourceSecrets{
			NameSelector:  &secretsv1beta1.NameSelector{MatchNames: []string{"registry-*"}},
			LabelSelector: &metav1.LabelSelector{},
		}, secret("registry-credentials", nil), true},
		{"every selector empty matches nothing", secretsv1beta1.SourceSecrets{
			NameSelector:  &secretsv1beta1.NameSelector{MatchNames: []string{}},
			UIDSelector:   &secretsv1beta1.UIDSelector{MatchUIDs: []string{}},
			OwnerSelector: &secretsv1beta1.OwnerSelector{MatchOwners: []secretsv1beta1.OwnerReference{}},
			LabelSelector: &metav1.LabelSelector{},
		}, secret("registry-credentials", nil), false},
		{"a glob", names("*-pull-secret"), secret("first-pull-secret", nil), true},
		{"a name the glob does not match", names("*-pull-secret"), secret("registry-credentials", nil), false},
		{"a name a ! pattern excludes", names("*-pull-secret", "!skip-pull-secret"),
			secret("skip-pull-secret", nil), false},
		{"only exclusions pass every other name", names("!skip-pull-secret"), secret("first-pull-secret", nil), true},
		{"its UID", uids("secret-uid"), secret("registry-credentials", nil), true},
		{"another UID", uids("other-uid"), secret("registry-credentials", nil), false},
		{"its owner", owners(configMapOwner), secret("registry-credentials", nil), true},
		{"an owner differing only in its UID", owners(secretsv1beta1.OwnerReference{
			APIVersion: "v1", Kind: "ConfigMap", Name: "owner", UID: "other-uid",
		}), secret("registry-credentials", nil), false},
		{"matchLabels", labelled(metav1.LabelSelector{MatchLabels: map[string]string{"inject": "yes"}}),
			secret("registry-credentials", map[string]string{"inject": "yes"}), true},
		{"matchLabels the Secret lacks", labelled(metav1.LabelSelector{MatchLabels: map[string]string{"inject": "yes"}}),
			secret("registry-credentials", nil), false},
		{"an In expression", labelled(metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
			requirement("inject", metav1.LabelSelectorOpIn, "yes", "always")}}),
			secret("registry-credentials", map[string]string{"inject": "always"}), true},
		{"an In expression without values matches nothing", labelled(metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{requirement("inject", metav1.LabelSelectorOpIn)}}),
			secret("registry-credentials", map[string]string{"inject": "yes"}), false},
		{"every selector passing", secretsv1beta1.SourceSecrets{
			NameSelector:  &secretsv1beta1.NameSelector{MatchNames: []string{"registry-*"}},
			UIDSelector:   &secretsv1beta1.UIDSelector{MatchUIDs: []string{"secret-uid"}},
			OwnerSelector: &secretsv1beta1.OwnerSelector{MatchOwners: []secretsv1beta1.OwnerReference{configMapOwner}},
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"inject": "yes"}},
		}, secret("registry-credentials", map[string]string{"inject": "yes"}), true},
		{"one selector of several failing", secretsv1beta1.SourceSecrets{
			NameSelector: &secretsv1beta1.NameSelector{MatchNames: []string{"registry-*"}},
			UIDSelector:  &secretsv1beta1.UIDSelector{MatchUIDs: []string{"other-uid"}},
		}, secret("registry-credentials", nil), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SourceSecrets(test.sources).Match(test.secret); got != test.want {
				t.Errorf("SourceSecrets(%+v).Match(%q) = %v, want %v", test.sources, test.secret.Name, got, test.want)
			}
		})
	}
}

func TestSourceSecretsErr(t *testing.T) {
	invalid := secretsv1beta1.SourceSecrets{LabelSelector: &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{requirement("inject", metav1.LabelSelectorOpNotIn)},
	}}
	if SourceSecrets(invalid).Err() == nil {
		t.Error("a NotIn expression without values is valid, want invalid")
	}
	valid := secretsv1beta1.SourceSecrets{NameSelector: &secretsv1beta1.NameSelector{MatchNames: []string{"*"}}}
	if err := SourceSecrets(valid).Err(); err != nil {
		t.Errorf("a name selector is invalid: %v", err)
	}
	emptyNames := secretsv1beta1.SourceSecrets{NameSelector: &secretsv1beta1.NameSelector{MatchNames: []string{}}}
	if SourceSecrets(emptyNames).Err() == nil {
		t.Error("an empty matchNames with no other selector is valid, want invalid")
	}
	emptyNames.UIDSelector = &secretsv1beta1.UIDSelector{MatchUIDs: []string{"secret-uid"}}
	if err := SourceSecrets(emptyNames).Err(); err != nil {
		t.Errorf("an empty matchNames beside a uidSelector is invalid: %v", err)
	}
}

func TestSourceSecretsErrWithOnlyEmptySelectors(t *testing.T) {
	tests := []struct {
		name    string
		sources secretsv1beta1.SourceSecrets
	}{
		{"an empty matchUIDs", secretsv1beta1.SourceSecrets{
			UIDSelector: &secretsv1beta1.UIDSelector{MatchUIDs: []string{}}}},
		{"an ownerSelector without matchOwners", secretsv1beta1.SourceSecrets{
			OwnerSelector: &secretsv1beta1.OwnerSelector{}}},
		{"an empty matchOwners", secretsv1beta1.SourceSecrets{
			OwnerSelector: &secretsv1beta1.OwnerSelector{MatchOwners: []secretsv1beta1.OwnerReference{}}}},
		{"an empty labelSelector", secretsv1beta1.SourceSecrets{LabelSelector: &metav1.LabelSelector{}}},
		{"every selector empty", secretsv1beta1.SourceSecrets{
			NameSelector:  &secretsv1beta1.NameSelector{MatchNames: []string{}},
			UIDSelector:   &secretsv1beta1.UIDSelector{MatchUIDs: []string{}},
			OwnerSelector: &secretsv1beta1.OwnerSelector{},
			LabelSelector: &metav1.LabelSelector{},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if SourceSecrets(test.sources).Err() == nil {
				t.Errorf("%s with no other selector is valid, want invalid", test.name)
			}
		})
	}

	beside := []struct {
		name    string
		sources secretsv1beta1.SourceSecrets
	}{
		{"an empty matchUIDs beside a labelSelector", secretsv1beta1.SourceSecrets{
			UIDSelector:   &secretsv1beta1.UIDSelector{MatchUIDs: []string{}},
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"inject": "yes"}},
		}},
		{"an empty matchOwners beside a uidSelector", secretsv1beta1.SourceSecrets{
			UIDSelector:   &secretsv1beta1.UIDSelector{MatchUIDs: []string{"secret-uid"}},
			OwnerSelector: &secretsv1beta1.OwnerSelector{},
		}},
		{"an empty labelSelector beside an ownerSelector", secretsv1beta1.SourceSecrets{
			OwnerSelector: &secretsv1beta1.OwnerSelector{MatchOwners: []secretsv1beta1.OwnerReference{{
				APIVersion: "v1", Kind: "ConfigMap", Name: "owner", UID: "owner-uid",
			}}},
			LabelSelector: &metav1.LabelSelector{},
		}},
		{"a labelSelector with only matchExpressions", secretsv1beta1.SourceSecrets{
			LabelSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
				requirement("inject", metav1.LabelSelectorOpExists)}},
		}},
	}
	for _, test := range beside {
		t.Run(test.name, func(t *testing.T) {
			if err := SourceSecrets(test.sources).Err(); err != nil {
				t.Errorf("%s is invalid: %v", test.name, err)
			}
		})
	}
}

func TestServiceAccountsMatch(t *testing.T) {
	account := func(name string, labels map[string]string) *corev1.ServiceAccount {
		return &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: name, Labels: labels}}
	}
	names := func(names ...string) *secretsv1beta1.ServiceAccounts {
		return &secretsv1beta1.ServiceAccounts{NameSelector: &secretsv1beta1.NameSelector{MatchNames: names}}
	}
	labelled := func(selector metav1.LabelSelector) *secretsv1beta1.ServiceAccounts {
		return &secretsv1beta1.ServiceAccounts{LabelSelector: &selector}
	}

	tests := []struct {
		name     string
		accounts *secretsv1beta1.ServiceAccounts
		account  *corev1.ServiceAccount
		want     bool
	}{
		{"no serviceAccounts passes every service account", nil, account("default", nil), true},
		{"serviceAccounts without selectors passes every service account", &secretsv1beta1.ServiceAccounts{},
			account("default", nil), true},
		{"an empty matchNames passes every name", names(), account("builder", nil), true},
		{"an exact name", names("builder", "runner"), account("runner", nil), true},
		{"another name", names("builder"), account("runner", nil), false},
		{"a pattern is not a glob", names("build*"), account("builder", nil), false},
		{"a pattern matches its own name only", names("build*"), account("build*", nil), true},
		{"a ! pattern is not an exclusion", names("!runner"), account("builder", nil), false},
		{"matchLabels", labelled(metav1.LabelSelector{MatchLabels: map[string]string{"role": "build"}}),
			account("builder", map[string]string{"role": "build"}), true},
		{"matchLabels the service account lacks", labelled(metav1.LabelSelector{
			MatchLabels: map[string]string{"role": "build"}}), account("builder", nil), false},
		{"an In expression", labelled(metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
			requirement("role", metav1.LabelSelectorOpIn, "build")}}),
			account("builder", map[string]string{"role": "build"}), true},
		{"an In expression without values matches nothing", labelled(metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{requirement("role", metav1.LabelSelectorOpIn)}}),
			account("builder", map[string]string{"role": "build"}), false},
		{"a name and labels both passing", &secretsv1beta1.ServiceAccounts{
			NameSelector:  &secretsv1beta1.NameSelector{MatchNames: []string{"builder"}},
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "build"}},
		}, account("builder", map[string]string{"role": "build"}), true},
		{"a name passing and labels failing", &secretsv1beta1.ServiceAccounts{
			NameSelector:  &secretsv1beta1.NameSelector{MatchNames: []string{"builder"}},
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"role": "build"}},
		}, account("builder", nil), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ServiceAccounts(test.accounts).Match(test.account); got != test.want {
				t.Errorf("ServiceAccounts(%+v).Match(%q) = %v, want %v", test.accounts, test.account.Name, got, test.want)
			}
		})
	}
}

func TestServiceAccountsErr(t *testing.T) {
	invalid := &secretsv1beta1.ServiceAccounts{LabelSelector: &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{requirement("role", metav1.LabelSelectorOpIn)},
	}}
	if ServiceAccounts(invalid).Err() == nil {
		t.Error("an In expression without values is valid, want invalid")
	}
	if err := ServiceAccounts(nil).Err(); err != nil {
		t.Errorf("no serviceAccounts is invalid: %v", err)
	}
}
