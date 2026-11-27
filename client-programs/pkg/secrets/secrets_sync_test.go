package secrets

import (
	"context"
	"strings"
	"testing"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestSyncIntoTheOperatorNamespace checks that the sync puts the cached
// Secrets into the operator namespace, educates-installer, creating it
// with the name label Helm gives a namespace it creates, so the operator
// chart's install finds the namespace it would have made.
func TestSyncIntoTheOperatorNamespace(t *testing.T) {
	t.Setenv("EDUCATES_CLI_DATA_HOME", t.TempDir())
	cached := NewDockerRegistrySecret("registry-credentials", "registry.example.com", "user", "pass", "")
	if err := WriteCachedSecret(cached); err != nil {
		t.Fatalf("WriteCachedSecret: %v", err)
	}
	client := fake.NewClientset()

	if err := SyncLocalCachedSecretsToCluster(client, []string{"registry-credentials"}); err != nil {
		t.Fatalf("SyncLocalCachedSecretsToCluster: %v", err)
	}

	namespace, err := client.CoreV1().Namespaces().Get(context.TODO(), "educates-installer", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("namespace educates-installer: %v", err)
	}
	if got := namespace.Labels["name"]; got != "educates-installer" {
		t.Errorf("namespace label name = %q, want educates-installer", got)
	}
	secret, err := client.CoreV1().Secrets("educates-installer").Get(context.TODO(), "registry-credentials", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Secret registry-credentials in educates-installer: %v", err)
	}
	if secret.Type != apiv1.SecretTypeDockerConfigJson {
		t.Errorf("Secret type = %q, want %q", secret.Type, apiv1.SecretTypeDockerConfigJson)
	}
	if _, err := client.CoreV1().Namespaces().Get(context.TODO(), "educates-secrets", metav1.GetOptions{}); err == nil {
		t.Errorf("the sync created the retired educates-secrets namespace")
	}
}

// TestRetiredSyncNamespaceNote checks that a cluster which still has the
// namespace earlier releases synced the cache into gets a note saying how
// to remove it, and that one without it gets none.
func TestRetiredSyncNamespaceNote(t *testing.T) {
	note, err := RetiredSyncNamespaceNote(fake.NewClientset())
	if err != nil {
		t.Fatalf("RetiredSyncNamespaceNote: %v", err)
	}
	if note != "" {
		t.Errorf("note for a cluster without educates-secrets = %q, want none", note)
	}

	leftover := &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "educates-secrets"}}
	note, err = RetiredSyncNamespaceNote(fake.NewClientset(leftover))
	if err != nil {
		t.Fatalf("RetiredSyncNamespaceNote: %v", err)
	}
	for _, want := range []string{"educates-secrets", "educates-installer", "kubectl delete namespace educates-secrets"} {
		if !strings.Contains(note, want) {
			t.Errorf("note %q does not mention %q", note, want)
		}
	}
}
