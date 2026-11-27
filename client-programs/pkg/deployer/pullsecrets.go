package deployer

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

// pullSecretProblem says what is wrong with one image pull Secret the
// operator namespace should hold.
type pullSecretProblem struct {
	name    string
	problem string
}

// pullSecretProblems returns a problem for each of names that the
// operator namespace does not hold as a kubernetes.io/dockerconfigjson
// Secret, in the order of names.
func pullSecretProblems(client kubernetes.Interface, names []string) ([]pullSecretProblem, error) {
	var problems []pullSecretProblem
	for _, name := range names {
		secret, err := client.CoreV1().Secrets(OperatorNamespace).Get(context.TODO(), name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			problems = append(problems, pullSecretProblem{name, "missing"})
		case err != nil:
			return nil, fmt.Errorf("read image pull secret %q in namespace %s: %w", name, OperatorNamespace, err)
		case secret.Type != corev1.SecretTypeDockerConfigJson:
			problems = append(problems, pullSecretProblem{name,
				fmt.Sprintf("of type %q, not %q", secret.Type, corev1.SecretTypeDockerConfigJson)})
		}
	}
	return problems, nil
}

// checkPullSecrets checks that the operator namespace holds each image
// pull Secret of names as a kubernetes.io/dockerconfigjson Secret. The
// operator pod pulls its own image with them and the EducatesClusterConfig
// is not Ready until each one exists, so a deploy checks them before it
// installs anything, and its error gives the commands that create the
// namespace and each Secret.
func checkPullSecrets(client kubernetes.Interface, names []string) error {
	problems, err := pullSecretProblems(client, names)
	if err != nil || len(problems) == 0 {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "the operator namespace %s must hold the image pull secrets imageRegistry.pullSecrets names, "+
		"as secrets of type %q, before the deploy:", OperatorNamespace, corev1.SecretTypeDockerConfigJson)
	for _, p := range problems {
		fmt.Fprintf(&b, "\n  %q is %s", p.name, p.problem)
	}
	fmt.Fprintf(&b, "\ncreate them with:\n  kubectl create namespace %s", OperatorNamespace)
	for _, p := range problems {
		fmt.Fprintf(&b, "\n  kubectl -n %s create secret docker-registry %s --docker-server=<registry> "+
			"--docker-username=<username> --docker-password=<password>", OperatorNamespace, p.name)
	}
	return fmt.Errorf("%s", b.String())
}

// clientsetFor returns a typed client of the cluster getter reaches.
func clientsetFor(getter genericclioptions.RESTClientGetter) (kubernetes.Interface, error) {
	cfg, err := getter.ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("REST config: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}
	return cs, nil
}
