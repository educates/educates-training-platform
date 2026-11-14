package educatesrestapi

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

// deployedPortal returns a TrainingPortal named lab-portal whose status
// says it was deployed to the lab-portal-ui namespace and names its
// credentials Secrets as session-manager does.
func deployedPortal() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "training.educates.dev/v1beta1",
		"kind":       "TrainingPortal",
		"metadata":   map[string]interface{}{"name": "lab-portal"},
		"status": map[string]interface{}{
			"educates": map[string]interface{}{
				"phase":     "Running",
				"namespace": "lab-portal-ui",
				"url":       "https://lab-portal-ui.example.test",
				"credentialSecrets": map[string]interface{}{
					"admin": "training-portal-admin-credentials",
					"robot": "training-portal-robot-credentials",
				},
			},
		},
	}}
}

func credentialsSecret(name string, data map[string]string) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "lab-portal-ui"},
		Data:       map[string][]byte{},
	}
	for key, value := range data {
		secret.Data[key] = []byte(value)
	}
	return secret
}

func portalCluster(objects ...runtime.Object) *fake.Clientset {
	return fake.NewClientset(objects...)
}

func TestReadPortalRobotCredentialsFromTheSecretTheStatusNames(t *testing.T) {
	client := portalCluster(credentialsSecret("training-portal-robot-credentials", map[string]string{
		"username":      "robot@educates",
		"password":      "robot-password",
		"client-id":     "robot-client-id",
		"client-secret": "robot-client-secret",
	}))

	got, err := ReadPortalRobotCredentials(context.Background(), client, deployedPortal())
	if err != nil {
		t.Fatalf("ReadPortalRobotCredentials: %v", err)
	}

	want := PortalRobotCredentials{
		Username:     "robot@educates",
		Password:     "robot-password",
		ClientID:     "robot-client-id",
		ClientSecret: "robot-client-secret",
	}
	if got != want {
		t.Errorf("credentials = %+v, want %+v", got, want)
	}
}

func TestReadPortalAdminCredentialsFromTheSecretTheStatusNames(t *testing.T) {
	client := portalCluster(credentialsSecret("training-portal-admin-credentials", map[string]string{
		"username": "educates",
		"password": "admin-password",
	}))

	got, err := ReadPortalAdminCredentials(context.Background(), client, deployedPortal())
	if err != nil {
		t.Fatalf("ReadPortalAdminCredentials: %v", err)
	}

	want := PortalAdminCredentials{Username: "educates", Password: "admin-password"}
	if got != want {
		t.Errorf("credentials = %+v, want %+v", got, want)
	}
}

func TestReadPortalCredentialsBeforeTheStatusNamesASecret(t *testing.T) {
	notDeployed := deployedPortal()
	unstructured.RemoveNestedField(notDeployed.Object, "status")

	notNamed := deployedPortal()
	unstructured.RemoveNestedField(notNamed.Object, "status", "educates", "credentialSecrets")

	for name, portal := range map[string]*unstructured.Unstructured{
		"no status":               notDeployed,
		"status names no Secrets": notNamed,
	} {
		t.Run(name, func(t *testing.T) {
			client := portalCluster()

			_, err := ReadPortalRobotCredentials(context.Background(), client, portal)
			assertErrorMentions(t, err, `training portal "lab-portal"`, "does not yet name its robot credentials Secret")

			_, err = ReadPortalAdminCredentials(context.Background(), client, portal)
			assertErrorMentions(t, err, `training portal "lab-portal"`, "does not yet name its admin credentials Secret")
		})
	}
}

func TestReadPortalCredentialsWhenTheNamedSecretIsMissing(t *testing.T) {
	client := portalCluster()

	_, err := ReadPortalRobotCredentials(context.Background(), client, deployedPortal())
	assertErrorMentions(t, err,
		`robot credentials Secret "training-portal-robot-credentials"`,
		`training portal "lab-portal"`,
		`does not exist in namespace "lab-portal-ui"`)

	_, err = ReadPortalAdminCredentials(context.Background(), client, deployedPortal())
	assertErrorMentions(t, err,
		`admin credentials Secret "training-portal-admin-credentials"`,
		`training portal "lab-portal"`,
		`does not exist in namespace "lab-portal-ui"`)
}

func TestReadPortalCredentialsWithoutSecretReadAccess(t *testing.T) {
	client := portalCluster(
		credentialsSecret("training-portal-admin-credentials", map[string]string{"username": "educates", "password": "admin-password"}),
		credentialsSecret("training-portal-robot-credentials", map[string]string{"username": "robot@educates", "password": "robot-password"}),
	)
	client.PrependReactor("get", "secrets", func(action k8stesting.Action) (bool, runtime.Object, error) {
		get := action.(k8stesting.GetAction)
		return true, nil, k8serrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, get.GetName(),
			errors.New(`User "jane" cannot get resource "secrets" in API group "" in the namespace "lab-portal-ui"`))
	})

	_, err := ReadPortalRobotCredentials(context.Background(), client, deployedPortal())
	assertErrorMentions(t, err,
		`robot credentials Secret "training-portal-robot-credentials"`,
		`needs "get" access on Secrets in namespace "lab-portal-ui"`,
		`User "jane" cannot get resource "secrets"`)

	_, err = ReadPortalAdminCredentials(context.Background(), client, deployedPortal())
	assertErrorMentions(t, err,
		`admin credentials Secret "training-portal-admin-credentials"`,
		`needs "get" access on Secrets in namespace "lab-portal-ui"`,
		`User "jane" cannot get resource "secrets"`)
}

func TestReadPortalCredentialsFromASecretMissingAKey(t *testing.T) {
	client := portalCluster(
		credentialsSecret("training-portal-admin-credentials", map[string]string{"username": "educates"}),
		credentialsSecret("training-portal-robot-credentials", map[string]string{
			"username":  "robot@educates",
			"password":  "robot-password",
			"client-id": "robot-client-id",
		}),
	)

	_, err := ReadPortalRobotCredentials(context.Background(), client, deployedPortal())
	assertErrorMentions(t, err,
		`robot credentials Secret "training-portal-robot-credentials"`,
		`has no "client-secret" key`)

	_, err = ReadPortalAdminCredentials(context.Background(), client, deployedPortal())
	assertErrorMentions(t, err,
		`admin credentials Secret "training-portal-admin-credentials"`,
		`has no "password" key`)
}

func assertErrorMentions(t *testing.T, err error, fragments ...string) {
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
