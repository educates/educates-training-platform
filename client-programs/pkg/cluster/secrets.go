package cluster

import (
	"context"

	"github.com/pkg/errors"
	apiv1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ReadReferencedSecret returns the Secret name in namespace, which referrer,
// such as `training portal "lab-portal"`, names as holding its contents,
// such as "admin credentials". The error when the Secret cannot be read says
// whether it does not exist or the cluster identity the command uses lacks
// "get" access on Secrets in the namespace, describing the Secret by its
// contents and referrer.
func ReadReferencedSecret(ctx context.Context, client kubernetes.Interface, referrer, contents, namespace, name string) (*apiv1.Secret, error) {
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		return nil, errors.Errorf("the %s Secret %q that %s names does not exist in namespace %q", contents, name, referrer, namespace)
	case apierrors.IsForbidden(err):
		return nil, errors.Wrapf(err, "cannot read the %s Secret %q of %s: the cluster identity this command uses needs \"get\" access on Secrets in namespace %q", contents, name, referrer, namespace)
	case err != nil:
		return nil, errors.Wrapf(err, "cannot read the %s Secret %q of %s", contents, name, referrer)
	}

	return secret, nil
}
