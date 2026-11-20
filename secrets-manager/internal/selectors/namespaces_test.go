package selectors

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
)

func TestNamespacesMatch(t *testing.T) {
	names := func(patterns ...string) secretsv1beta1.TargetNamespaces {
		return secretsv1beta1.TargetNamespaces{NameSelector: &secretsv1beta1.NameSelector{MatchNames: patterns}}
	}
	tests := []struct {
		name      string
		selector  secretsv1beta1.TargetNamespaces
		namespace string
		want      bool
	}{
		{"without a name selector, any namespace", secretsv1beta1.TargetNamespaces{}, "apps", true},
		{"without a name selector, no kube- namespace", secretsv1beta1.TargetNamespaces{}, "kube-system", false},
		{"a listed name", names("apps", "web"), "web", true},
		{"a name not listed", names("apps", "web"), "db", false},
		{"a glob", names("team-*"), "team-a", true},
		{"an exclusion", names("team-*", "!team-b"), "team-b", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: test.namespace}}
			if got := Namespaces(test.selector).Match(ns); got != test.want {
				t.Errorf("Namespaces(%+v).Match(%q) = %v, want %v", test.selector, test.namespace, got, test.want)
			}
		})
	}
}
