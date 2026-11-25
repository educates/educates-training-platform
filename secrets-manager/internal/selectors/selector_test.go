package selectors

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
)

func TestNewWithoutDefaultNames(t *testing.T) {
	secret := func(name string, labels map[string]string) *corev1.Secret {
		return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: "apps", Name: name, Labels: labels}}
	}
	names := func(patterns ...string) Fields {
		return Fields{Names: &secretsv1beta1.NameSelector{MatchNames: patterns}}
	}
	tests := []struct {
		name   string
		fields Fields
		secret *corev1.Secret
		want   bool
	}{
		{"no selector passes any name", Fields{}, secret("kube-root-ca", nil), true},
		{"an empty matchNames passes any name", names(), secret("kube-root-ca", nil), true},
		{"a glob", names("*-pull-secret"), secret("registry-pull-secret", nil), true},
		{"a name the glob does not match", names("*-pull-secret"), secret("registry-credentials", nil), false},
		{"an exclusion", names("*-pull-secret", "!private-pull-secret"), secret("private-pull-secret", nil), false},
		{"a label selector", Fields{Labels: &metav1.LabelSelector{MatchLabels: map[string]string{"pull": "yes"}}},
			secret("registry", map[string]string{"pull": "yes"}), true},
		{"an invalid label selector", Fields{Labels: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{
			requirement("pull", metav1.LabelSelectorOpIn)}}}, secret("registry", map[string]string{"pull": "yes"}), false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := New(test.fields, nil).Match(test.secret); got != test.want {
				t.Errorf("New(%+v, nil).Match(%q) = %v, want %v", test.fields, test.secret.Name, got, test.want)
			}
		})
	}
}
