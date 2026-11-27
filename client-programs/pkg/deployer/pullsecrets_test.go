package deployer

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func pullSecret(name string, secretType corev1.SecretType) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "educates-installer"},
		Type:       secretType,
	}
}

func TestCheckOperatorNamespacePullSecrets_PassesWhenEachIsARegistryCredentialInTheOperatorNamespace(t *testing.T) {
	client := fake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "educates-installer"}},
		pullSecret("mirror", corev1.SecretTypeDockerConfigJson),
	)
	if err := checkOperatorNamespacePullSecrets(client, []string{"mirror"}); err != nil {
		t.Errorf("checkOperatorNamespacePullSecrets: %v", err)
	}
}

func TestCheckOperatorNamespacePullSecrets_NamesWhatIsMissingAndHowToCreateIt(t *testing.T) {
	cases := []struct {
		name    string
		objects []any
		says    []string
	}{
		{
			"no operator namespace", nil,
			[]string{`"mirror"`, "kubectl create namespace educates-installer",
				"kubectl -n educates-installer create secret docker-registry mirror"},
		},
		{
			"no Secret",
			[]any{&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "educates-installer"}}},
			[]string{`"mirror"`, "kubectl -n educates-installer create secret docker-registry mirror"},
		},
		{
			"another type",
			[]any{
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "educates-installer"}},
				pullSecret("mirror", corev1.SecretTypeOpaque),
			},
			[]string{`"mirror"`, "kubernetes.io/dockerconfigjson"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := fake.NewClientset()
			for _, o := range c.objects {
				switch obj := o.(type) {
				case *corev1.Namespace:
					_ = client.Tracker().Add(obj)
				case *corev1.Secret:
					_ = client.Tracker().Add(obj)
				}
			}
			err := checkOperatorNamespacePullSecrets(client, []string{"mirror"})
			if err == nil {
				t.Fatal("checkOperatorNamespacePullSecrets accepted a pull secret the operator namespace does not hold")
			}
			for _, want := range c.says {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %s", err, want)
				}
			}
		})
	}
}
