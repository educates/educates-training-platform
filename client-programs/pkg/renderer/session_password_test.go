package renderer

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// portalSession returns the WorkshopSession lab-portal-w01-s001 of the
// workshop environment lab-portal-w01, with the given spec.session.config.
func portalSession(config map[string]interface{}) *unstructured.Unstructured {
	session := map[string]interface{}{"id": "s001"}
	if config != nil {
		session["config"] = config
	}

	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "training.educates.dev/v1beta1",
		"kind":       "WorkshopSession",
		"metadata":   map[string]interface{}{"name": "lab-portal-w01-s001"},
		"spec": map[string]interface{}{
			"workshop":    map[string]interface{}{"name": "lab-workshop"},
			"portal":      map[string]interface{}{"name": "lab-portal"},
			"environment": map[string]interface{}{"name": "lab-portal-w01"},
			"session":     session,
		},
	}}
}

func referencedPassword() map[string]interface{} {
	return map[string]interface{}{
		"passwordSecretRef": map[string]interface{}{"name": "lab-portal-w01-s001-config-password"},
	}
}

func passwordSecret(data map[string]string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "lab-portal-w01-s001-config-password", Namespace: "lab-portal-w01"},
		Data:       map[string][]byte{},
	}
	for key, value := range data {
		secret.Data[key] = []byte(value)
	}
	return secret
}

func sessionCluster(objects ...runtime.Object) *fake.Clientset {
	return fake.NewClientset(objects...)
}

func TestSessionConfigPasswordReadsTheSecretTheSessionNames(t *testing.T) {
	client := sessionCluster(passwordSecret(map[string]string{"password": "from-the-secret"}))

	got, err := sessionConfigPassword(context.Background(), client, portalSession(referencedPassword()))
	if err != nil {
		t.Fatalf("sessionConfigPassword: %v", err)
	}

	if got != "from-the-secret" {
		t.Errorf("password = %q, want %q", got, "from-the-secret")
	}
}

func TestSessionConfigPasswordPrefersTheSecretToAPasswordGivenInTheSpec(t *testing.T) {
	client := sessionCluster(passwordSecret(map[string]string{"password": "from-the-secret"}))
	config := referencedPassword()
	config["password"] = "given-in-the-spec"

	got, err := sessionConfigPassword(context.Background(), client, portalSession(config))
	if err != nil {
		t.Fatalf("sessionConfigPassword: %v", err)
	}

	if got != "from-the-secret" {
		t.Errorf("password = %q, want %q", got, "from-the-secret")
	}
}

func TestSessionConfigPasswordTakesAPasswordGivenInTheSpec(t *testing.T) {
	client := sessionCluster()

	got, err := sessionConfigPassword(context.Background(), client, portalSession(map[string]interface{}{"password": "given-in-the-spec"}))
	if err != nil {
		t.Fatalf("sessionConfigPassword: %v", err)
	}

	if got != "given-in-the-spec" {
		t.Errorf("password = %q, want %q", got, "given-in-the-spec")
	}
}

func TestSessionConfigPasswordWhenTheNamedSecretIsMissing(t *testing.T) {
	client := sessionCluster()

	_, err := sessionConfigPassword(context.Background(), client, portalSession(referencedPassword()))
	assertPasswordErrorMentions(t, err,
		`config password Secret "lab-portal-w01-s001-config-password"`,
		`workshop session "lab-portal-w01-s001"`,
		`does not exist in namespace "lab-portal-w01"`)
}

func TestSessionConfigPasswordWithoutSecretReadAccess(t *testing.T) {
	client := sessionCluster(passwordSecret(map[string]string{"password": "from-the-secret"}))
	client.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		get := action.(k8stesting.GetAction)
		return true, nil, k8serrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, get.GetName(),
			errors.New(`User "jane" cannot get resource "secrets" in API group "" in the namespace "lab-portal-w01"`))
	})

	_, err := sessionConfigPassword(context.Background(), client, portalSession(referencedPassword()))
	assertPasswordErrorMentions(t, err,
		`config password Secret "lab-portal-w01-s001-config-password"`,
		`workshop session "lab-portal-w01-s001"`,
		`needs "get" access on Secrets in namespace "lab-portal-w01"`,
		`User "jane" cannot get resource "secrets"`)
}

func TestSessionConfigPasswordFromASecretWithoutThePasswordKey(t *testing.T) {
	client := sessionCluster(passwordSecret(map[string]string{"other": "value"}))

	_, err := sessionConfigPassword(context.Background(), client, portalSession(referencedPassword()))
	assertPasswordErrorMentions(t, err,
		`config password Secret "lab-portal-w01-s001-config-password"`,
		`workshop session "lab-portal-w01-s001"`,
		`has no "password" key`)
}

func assertPasswordErrorMentions(t *testing.T, err error, fragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want one mentioning %q", fragments)
	}
	for _, fragment := range fragments {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("error %q does not mention %q", err, fragment)
		}
	}
}
