package renderer

import (
	"context"

	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
)

// configPasswordKey is the key of the Secret that holds a workshop
// session's config password.
const configPasswordKey = "password"

// sessionConfigPassword returns the config password of a workshop session,
// which unlocks the session's configuration from its workshop gateway. A
// training portal passes it in the Secret that
// spec.session.config.passwordSecretRef.name names, under the key password,
// in the namespace of the session's workshop environment, which is named
// after the environment. Reading it needs "get" access on Secrets there. A
// session made by hand may give the password in spec.session.config.password
// instead; a reference takes precedence, as it does for session-manager. A
// session giving neither returns an empty password.
func sessionConfigPassword(ctx context.Context, client kubernetes.Interface, workshopSession *unstructured.Unstructured) (string, error) {
	name, _, _ := unstructured.NestedString(workshopSession.Object, "spec", "session", "config", "passwordSecretRef", "name")

	if name == "" {
		password, _, _ := unstructured.NestedString(workshopSession.Object, "spec", "session", "config", "password")
		return password, nil
	}

	namespace, _, _ := unstructured.NestedString(workshopSession.Object, "spec", "environment", "name")

	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	switch {
	case k8serrors.IsNotFound(err):
		return "", errors.Errorf("the config password Secret %q that workshop session %q names does not exist in namespace %q", name, workshopSession.GetName(), namespace)
	case k8serrors.IsForbidden(err):
		return "", errors.Wrapf(err, "cannot read the config password Secret %q of workshop session %q: the cluster identity this command uses needs \"get\" access on Secrets in namespace %q", name, workshopSession.GetName(), namespace)
	case err != nil:
		return "", errors.Wrapf(err, "cannot read the config password Secret %q of workshop session %q", name, workshopSession.GetName())
	}

	password, found := secret.Data[configPasswordKey]
	if !found {
		return "", errors.Errorf("the config password Secret %q of workshop session %q has no %q key", name, workshopSession.GetName(), configPasswordKey)
	}

	return string(password), nil
}
