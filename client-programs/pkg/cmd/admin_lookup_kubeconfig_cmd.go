package cmd

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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

  # Write the kubeconfig of the local cluster "remote", which names port
  # 6443 of the router address, where the router serves its API server to
  # this host and to the pods of every local cluster:
  educates admin lookup kubeconfig --local-cluster remote --output kubeconfig-remote.yaml

  # Name the address a lookup service on another cluster reaches the API
  # server at:
  educates admin lookup kubeconfig --context cluster-1 --server https://api.cluster-1.example.com:6443

  # Name the API server at an address its certificate does not name, and
  # the name the certificate is verified against:
  educates admin lookup kubeconfig --context cluster-1 --server https://10.0.0.5:6443 --tls-server-name api.cluster-1.internal

  # Name the proxy a lookup service on another cluster reaches the API
  # server through:
  educates admin lookup kubeconfig --context cluster-1 --proxy-url http://proxy.example.com:3128
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
	OutputPath    string
	Namespace     string
	Server        string
	TLSServerName string
	ProxyURL      string
}

// Run writes the kubeconfig, and then writes to stderr what the kubeconfig
// leaves out of the kube context's way to the API server.
func (o *LookupConfigOptions) Run(ctx context.Context, stderr io.Writer) error {
	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)
	if err != nil {
		return err
	}

	restConfig, err := clusterConfig.GetConfig()
	if err != nil {
		return err
	}

	kubeconfigCluster, note, err := o.kubeconfigCluster(cmp.Or(o.Context, cluster.CurrentContext(o.Kubeconfig)),
		restConfig, utils.GetEducatesHomeDir())
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

	kubeconfig, err := remoteAccessKubeconfig(kubeconfigCluster, credentials)
	if err != nil {
		return err
	}

	if o.OutputPath == "" {
		_, err = os.Stdout.Write(kubeconfig)
	} else {
		err = errors.Wrapf(writeOwnerOnlyFile(o.OutputPath, kubeconfig), "unable to write kubeconfig to %s",
			o.OutputPath)
	}
	if err == nil && note != "" {
		fmt.Fprintln(stderr, note)
	}
	return err
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

// kubeconfigServer is how the kubeconfig names the API server: its
// address, the name its certificate is verified against, and a note on
// whom that address leaves out.
type kubeconfigServer struct {
	// Server is the API server's address.
	Server string

	// TLSServerName is the name the API server's certificate is verified
	// against in place of the host of Server, empty for that host.
	TLSServerName string

	// Note says whom Server does not reach the API server for, empty when
	// it reaches it for every lookup service it is meant for.
	Note string
}

// lookupKubeconfigServer returns how the kubeconfig names the API server.
//
//   - With server, from --server, it is server, which must be an https URL
//     with a host, with no tls-server-name, as kubectl too drops the
//     tls-server-name of a kube context whose server --server overrides.
//   - Without it, when kubeContext, the kube context the command reads the
//     token through, is the context of a local cluster of the data home
//     dataHome, as localclusters.ClusterOfContext recognises it, it is the
//     router's address for the cluster's API server, which the host and
//     the pods of every local cluster reach: port 6443 of the router
//     address, with the cluster's control-plane node as tls-server-name,
//     as localclusters.RouterAPIServerOf gives it. The kube context reaches
//     the API server at a loopback port of the host, which no pod reaches.
//     When the router serves nothing of the cluster, it is the cluster's
//     address on Docker's kind network, which only the pods of the other
//     local clusters reach, and the note says so, and why.
//   - Otherwise it is the server of kubeContext, whose client
//     configuration is contextConfig, with its tls-server-name.
func lookupKubeconfigServer(server, kubeContext string, contextConfig *rest.Config,
	dataHome string) (kubeconfigServer, error) {
	if server != "" {
		u, err := url.Parse(server)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return kubeconfigServer{}, errors.Errorf("invalid --server %q: the API server address is an https URL, "+
				"such as https://api.cluster-1.example.com:6443", server)
		}
		return kubeconfigServer{Server: server}, nil
	}
	id, ok := localclusters.ClusterOfContext(kubeContext, dataHome)
	if !ok {
		return kubeconfigServer{Server: contextConfig.Host, TLSServerName: contextConfig.ServerName}, nil
	}
	api, err := localclusters.RouterAPIServerOf(id)
	if err != nil {
		return kubeconfigServer{
			Server: id.KindNetworkAPIServerURL(),
			Note: fmt.Sprintf("Note: the kubeconfig names %s, the address of %s on Docker's kind network, which "+
				"only the pods of the other local clusters reach, since the router, which this host reaches "+
				"too, does not serve its API server: %v", id.KindNetworkAPIServerURL(), id.Description(), err),
		}, nil
	}
	return kubeconfigServer{Server: api.URL, TLSServerName: api.ServerName}, nil
}

// kubeconfigCluster returns the cluster entry of the kubeconfig, but for
// its CA certificate, which the remote-access-token Secret holds, and a note
// to print when the kubeconfig leaves out part of the way the kube context
// kubeContext, whose client configuration is contextConfig, reaches the API
// server, or reaches it for fewer clients than it is meant for.
//
// Its server and tls-server-name are lookupKubeconfigServer's, but for a
// tls-server-name --tls-server-name gives, which wins.
//
// Its proxy-url is the one --proxy-url gives, which must be an http, https
// or socks5 URL with a host. The kube context's proxy is never copied, since
// a lookup service seldom reaches the proxy of a kube context, often a
// tunnel on the loopback address, and the note says it is left out.
func (o *LookupConfigOptions) kubeconfigCluster(kubeContext string, contextConfig *rest.Config,
	dataHome string) (*clientcmdapi.Cluster, string, error) {
	server, err := lookupKubeconfigServer(o.Server, kubeContext, contextConfig, dataHome)
	if err != nil {
		return nil, "", err
	}
	entry := &clientcmdapi.Cluster{Server: server.Server, TLSServerName: cmp.Or(o.TLSServerName, server.TLSServerName)}

	if o.ProxyURL != "" {
		// The error leaves the URL out, since it may hold the proxy's
		// credentials.
		u, err := url.Parse(o.ProxyURL)
		if err != nil || u.Host == "" || u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" {
			return nil, "", errors.New("invalid --proxy-url: the proxy is an http, https or socks5 URL with a " +
				"host, such as http://proxy.example.com:3128")
		}
		entry.ProxyURL = o.ProxyURL
	}
	var notes []string
	if server.Note != "" {
		notes = append(notes, server.Note)
	}
	if o.ProxyURL == "" && contextConfig.Proxy != nil {
		notes = append(notes, "Note: the kube context reaches the API server through a proxy, which the kubeconfig "+
			"leaves out, since a lookup service seldom reaches the proxy of a kube context. If the lookup service "+
			"reaches the API server through a proxy, name that proxy with --proxy-url.")
	}
	return entry, strings.Join(notes, "\n"), nil
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
// the way the cluster entry gives as the remote-access service account,
// with credentials, whose CA certificate the entry gives.
func remoteAccessKubeconfig(entry *clientcmdapi.Cluster, credentials remoteAccessCredentials) ([]byte, error) {
	config := clientcmdapi.NewConfig()
	cluster := entry.DeepCopy()
	cluster.CertificateAuthorityData = credentials.CACertificate
	config.Clusters["training-platform"] = cluster
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
			return o.Run(cmd.Context(), cmd.ErrOrStderr())
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
		"API server address the kubeconfig names, an https URL; by default port 6443 of the router address for "+
			"a local cluster, and otherwise the server of the kube context",
	)
	c.Flags().StringVar(
		&o.TLSServerName,
		"tls-server-name",
		"",
		"name the API server's certificate is verified against in place of the host of the server; by default, "+
			"without --server, the name of a local cluster's control-plane node, by which the router sends the "+
			"connection to it, and otherwise the kube context's",
	)
	c.Flags().StringVar(
		&o.ProxyURL,
		"proxy-url",
		"",
		"http, https or socks5 proxy the lookup service reaches the API server through; the kube context's "+
			"proxy is never copied",
	)

	return c
}
