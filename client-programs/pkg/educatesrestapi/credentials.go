package educatesrestapi

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
)

// PortalAdminCredentials are the username and password of a training
// portal's administrator account.
type PortalAdminCredentials struct {
	Username string
	Password string
}

// PortalRobotCredentials are the username, password and OAuth client id and
// secret a REST API client logs in to a training portal with.
type PortalRobotCredentials struct {
	Username     string
	Password     string
	ClientID     string
	ClientSecret string
}

// ReadPortalAdminCredentials returns a training portal's admin credentials,
// read from the Secret that status.educates.credentialSecrets.admin names,
// in the portal's namespace.
func ReadPortalAdminCredentials(ctx context.Context, client kubernetes.Interface, trainingPortal *unstructured.Unstructured) (PortalAdminCredentials, error) {
	values, err := readCredentialsSecret(ctx, client, trainingPortal, "admin", "username", "password")
	if err != nil {
		return PortalAdminCredentials{}, err
	}

	return PortalAdminCredentials{
		Username: values["username"],
		Password: values["password"],
	}, nil
}

// ReadPortalRobotCredentials returns a training portal's robot credentials,
// read from the Secret that status.educates.credentialSecrets.robot names,
// in the portal's namespace.
func ReadPortalRobotCredentials(ctx context.Context, client kubernetes.Interface, trainingPortal *unstructured.Unstructured) (PortalRobotCredentials, error) {
	values, err := readCredentialsSecret(ctx, client, trainingPortal, "robot", "username", "password", "client-id", "client-secret")
	if err != nil {
		return PortalRobotCredentials{}, err
	}

	return PortalRobotCredentials{
		Username:     values["username"],
		Password:     values["password"],
		ClientID:     values["client-id"],
		ClientSecret: values["client-secret"],
	}, nil
}

// readCredentialsSecret returns the values of the given keys of the
// credentials Secret a training portal's status names for the account,
// admin or robot. The Secret lives in the namespace the status names.
func readCredentialsSecret(ctx context.Context, client kubernetes.Interface, trainingPortal *unstructured.Unstructured, account string, keys ...string) (map[string]string, error) {
	name, _, _ := unstructured.NestedString(trainingPortal.Object, "status", "educates", "credentialSecrets", account)
	namespace, _, _ := unstructured.NestedString(trainingPortal.Object, "status", "educates", "namespace")

	if name == "" || namespace == "" {
		return nil, errors.Errorf("training portal %q does not yet name its %s credentials Secret in its status", trainingPortal.GetName(), account)
	}

	secret, err := cluster.ReadReferencedSecret(ctx, client,
		fmt.Sprintf("training portal %q", trainingPortal.GetName()), account+" credentials", namespace, name)
	if err != nil {
		return nil, err
	}

	values := map[string]string{}
	for _, key := range keys {
		value, found := secret.Data[key]
		if !found {
			return nil, errors.Errorf("the %s credentials Secret %q of training portal %q has no %q key", account, name, trainingPortal.GetName(), key)
		}
		values[key] = string(value)
	}

	return values, nil
}
