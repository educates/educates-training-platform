package secrets

import (
	"testing"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func previewSecret(name string, secretType apiv1.SecretType, data map[string]string) *apiv1.Secret {
	s := &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: LocalSecretsSyncNamespace},
		Type:       secretType,
		Data:       map[string][]byte{},
	}
	for key, value := range data {
		s.Data[key] = []byte(value)
	}
	return s
}

// TestPreviewSyncWithoutTheNamespace checks that every Secret would be
// created, with the namespace, when the namespace does not exist.
func TestPreviewSyncWithoutTheNamespace(t *testing.T) {
	client := fake.NewClientset()
	pending := map[string]*apiv1.Secret{
		"educates.test-ca-key-pair": previewSecret("educates.test-ca-key-pair", apiv1.SecretTypeTLS, map[string]string{"tls.crt": "c"}),
	}

	namespaceCreated, changes, err := PreviewSyncLocalCachedSecretsToCluster(client, []string{"educates.test-ca-key-pair"}, pending)
	if err != nil {
		t.Fatalf("PreviewSyncLocalCachedSecretsToCluster: %v", err)
	}
	if !namespaceCreated {
		t.Errorf("namespaceCreated = false, want true")
	}
	if len(changes) != 1 || changes[0].Change != SecretCreated {
		t.Errorf("changes = %+v, want the Secret created", changes)
	}
}

// TestPreviewSyncComparesTypeAndCachedKeys checks that a Secret is
// unchanged when the cluster holds its type and the value of each of its
// keys, from data or stringData, updated when either differs, and created
// when the cluster lacks it, and that the preview changes nothing.
func TestPreviewSyncComparesTypeAndCachedKeys(t *testing.T) {
	namespace := &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: LocalSecretsSyncNamespace}}
	client := fake.NewClientset(
		namespace,
		previewSecret("same", apiv1.SecretTypeTLS, map[string]string{"tls.crt": "c", "tls.key": "k", "extra": "x"}),
		previewSecret("same-string", "", map[string]string{"token": "t"}),
		previewSecret("other-value", apiv1.SecretTypeTLS, map[string]string{"tls.crt": "old"}),
		previewSecret("other-type", apiv1.SecretTypeOpaque, map[string]string{"tls.crt": "c"}),
	)

	stringData := previewSecret("same-string", apiv1.SecretTypeOpaque, nil)
	stringData.StringData = map[string]string{"token": "t"}

	pending := map[string]*apiv1.Secret{
		"same":        previewSecret("same", apiv1.SecretTypeTLS, map[string]string{"tls.crt": "c", "tls.key": "k"}),
		"same-string": stringData,
		"other-value": previewSecret("other-value", apiv1.SecretTypeTLS, map[string]string{"tls.crt": "new"}),
		"other-type":  previewSecret("other-type", apiv1.SecretTypeTLS, map[string]string{"tls.crt": "c"}),
		"missing":     previewSecret("missing", apiv1.SecretTypeOpaque, map[string]string{"a": "b"}),
	}
	names := []string{"same", "same-string", "other-value", "other-type", "missing"}

	namespaceCreated, changes, err := PreviewSyncLocalCachedSecretsToCluster(client, names, pending)
	if err != nil {
		t.Fatalf("PreviewSyncLocalCachedSecretsToCluster: %v", err)
	}
	if namespaceCreated {
		t.Errorf("namespaceCreated = true, want false")
	}

	want := map[string]string{
		"same":        SecretUnchanged,
		"same-string": SecretUnchanged,
		"other-value": SecretUpdated,
		"other-type":  SecretUpdated,
		"missing":     SecretCreated,
	}
	if len(changes) != len(want) {
		t.Fatalf("changes = %+v, want %d", changes, len(want))
	}
	for _, change := range changes {
		if change.Change != want[change.Name] {
			t.Errorf("%s: change = %q, want %q", change.Name, change.Change, want[change.Name])
		}
	}

	for _, action := range client.Actions() {
		if verb := action.GetVerb(); verb != "get" {
			t.Errorf("the preview made a %s request on %s, want only reads", verb, action.GetResource().Resource)
		}
	}
}
