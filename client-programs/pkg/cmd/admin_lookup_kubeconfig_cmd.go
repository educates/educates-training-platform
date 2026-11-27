package cmd

import (
	"cmp"
	"context"
	"net/url"
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var adminLookupKubeconfigExample = `
  # Print a kubeconfig for lookup-service remote access:
  educates admin lookup kubeconfig

  # Write the kubeconfig to a file:
  educates admin lookup kubeconfig --output kubeconfig.yaml

  # Write the kubeconfig of the local cluster "remote", which names the
  # address the pods of the other local clusters reach its API server at:
  educates admin lookup kubeconfig --local-cluster remote --output kubeconfig-remote.yaml

  # Name the address a lookup service on another cluster reaches the API
  # server at:
  educates admin lookup kubeconfig --context cluster-1 --server https://api.cluster-1.example.com:6443
`

const (
	// remoteAccessDefaultNamespace is the namespace the operator installs
	// remote access in, with the other runtime components.
	remoteAccessDefaultNamespace = "educates"

	// remoteAccessTokenSecretName is the name of the Secret remote access
	// installs, which Kubernetes fills with the token of the remote-access
	// service account and the CA certificate of the cluster.
	remoteAccessTokenSecretName = "remote-access-token"
)

type LookupConfigOptions struct {
	KubeconfigOptions
	OutputPath string
	Namespace  string
	Server     string
}

func (o *LookupConfigOptions) Run(ctx context.Context) error {
	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)
	if err != nil {
		return err
	}

	restConfig, err := clusterConfig.GetConfig()
	if err != nil {
		return err
	}

	server, err := lookupKubeconfigServer(o.Server, cmp.Or(o.Context, cluster.CurrentContext(o.Kubeconfig)),
		restConfig.Host, utils.GetEducatesHomeDir())
	if err != nil {
		return err
	}

	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return err
	}

	credentials, err := readRemoteAccessCredentials(ctx, client, o.Namespace)
	if err != nil {
		return err
	}

	kubeconfig, err := remoteAccessKubeconfig(server, credentials)
	if err != nil {
		return err
	}

	if o.OutputPath == "" {
		_, err = os.Stdout.Write(kubeconfig)
		return err
	}
	return errors.Wrapf(writeOwnerOnlyFile(o.OutputPath, kubeconfig), "unable to write kubeconfig to %s", o.OutputPath)
}

// writeOwnerOnlyFile writes data to the file path, which only its owner
// may read or write, since the kubeconfig holds the token. An existing
// file keeps its mode when it is opened, so the mode is set before data
// is written.
func writeOwnerOnlyFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// lookupKubeconfigServer returns the API server address the kubeconfig
// names.
//
//   - With server, from --server, it is server, which must be an https URL
//     with a host.
//   - Without it, when kubeContext, the kube context the command reads the
//     token through, is the context of a local cluster of the data home
//     dataHome, as localclusters.ClusterOfContext recognises it, it is the
//     URL at which the pods of the other local clusters reach the cluster.
//     The kube context reaches it at a loopback port of the host that no
//     pod reaches.
//   - Otherwise it is contextServer, the server of kubeContext.
func lookupKubeconfigServer(server, kubeContext, contextServer, dataHome string) (string, error) {
	if server != "" {
		u, err := url.Parse(server)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return "", errors.Errorf("invalid --server %q: the API server address is an https URL, "+
				"such as https://api.cluster-1.example.com:6443", server)
		}
		return server, nil
	}
	if id, ok := localclusters.ClusterOfContext(kubeContext, dataHome); ok {
		return id.KindNetworkAPIServerURL(), nil
	}
	return contextServer, nil
}

// remoteAccessCredentials is what the kubeconfig reaches a cluster with as
// the remote-access service account.
type remoteAccessCredentials struct {
	// CACertificate is the CA certificate of the cluster, which its API
	// server certificate chains to.
	CACertificate []byte

	// Token is the token of the remote-access service account.
	Token []byte
}

// readRemoteAccessCredentials returns the credentials of the remote-access
// service account, from the Secret remote access installs in namespace.
// The error says why when remote access is not installed there, or
// Kubernetes has not filled the Secret yet.
func readRemoteAccessCredentials(ctx context.Context, client kubernetes.Interface, namespace string) (remoteAccessCredentials, error) {
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, remoteAccessTokenSecretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return remoteAccessCredentials{}, errors.Errorf("remote access is not installed on the cluster: "+
			"namespace %q has no Secret %s. Install it by setting spec.remoteAccess.mode to Enabled on the "+
			"cluster's SessionManager, since at its default, Auto, remote access is installed only while a "+
			"LookupService exists on the cluster. For the runtime chart installed without the operator, "+
			"name the namespace it was installed in with --namespace",
			namespace, remoteAccessTokenSecretName)
	}
	if err != nil {
		return remoteAccessCredentials{}, errors.Wrapf(err, "unable to read Secret %s in namespace %q",
			remoteAccessTokenSecretName, namespace)
	}

	credentials := remoteAccessCredentials{CACertificate: secret.Data["ca.crt"], Token: secret.Data["token"]}
	if len(credentials.CACertificate) == 0 || len(credentials.Token) == 0 {
		return remoteAccessCredentials{}, errors.Errorf("Secret %s in namespace %q holds no token yet: "+
			"Kubernetes adds the token of the remote-access service account to it shortly after remote "+
			"access is installed, so try again", remoteAccessTokenSecretName, namespace)
	}
	return credentials, nil
}

// remoteAccessKubeconfig returns a kubeconfig that reaches the API server
// at server as the remote-access service account, with credentials.
func remoteAccessKubeconfig(server string, credentials remoteAccessCredentials) ([]byte, error) {
	config := clientcmdapi.NewConfig()
	config.Clusters["training-platform"] = &clientcmdapi.Cluster{
		Server:                   server,
		CertificateAuthorityData: credentials.CACertificate,
	}
	config.AuthInfos["remote-access"] = &clientcmdapi.AuthInfo{
		Token: string(credentials.Token),
	}
	config.Contexts["training-platform"] = &clientcmdapi.Context{
		Cluster:  "training-platform",
		AuthInfo: "remote-access",
	}
	config.CurrentContext = "training-platform"

	kubeconfig, err := clientcmd.Write(*config)
	if err != nil {
		return nil, errors.Wrap(err, "unable to write kubeconfig")
	}
	return kubeconfig, nil
}

func (p *ProjectInfo) NewAdminLookupKubeconfigCmd() *cobra.Command {
	var o LookupConfigOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "kubeconfig",
		Short:   "Fetch kubeconfig for lookup service remote access",
		Example: adminLookupKubeconfigExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return o.Run(cmd.Context())
		},
	}

	c.Flags().StringVar(
		&o.Kubeconfig,
		"kubeconfig",
		"",
		"kubeconfig file to use instead of $KUBECONFIG or $HOME/.kube/config",
	)
	c.Flags().StringVar(
		&o.Context,
		"context",
		"",
		"Context to use from Kubeconfig",
	)
	addLocalClusterContextFlag(c, &o.KubeconfigOptions)
	c.Flags().StringVarP(
		&o.OutputPath,
		"output",
		"o",
		"",
		"Path to write Kubeconfig file to",
	)
	c.Flags().StringVar(
		&o.Namespace,
		"namespace",
		remoteAccessDefaultNamespace,
		"namespace remote access is installed in, which holds the "+remoteAccessTokenSecretName+" Secret",
	)
	c.Flags().StringVar(
		&o.Server,
		"server",
		"",
		"API server address the kubeconfig names, an https URL; by default the address the pods of the other "+
			"local clusters reach a local cluster at, and otherwise the server of the kube context",
	)

	return c
}
