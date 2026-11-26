package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/spf13/cobra"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localClusterDeleteExample = `
  # Delete the local cluster:
  educates local cluster delete

  # Delete the named local cluster test2, keeping its config:
  educates local cluster delete --local-cluster test2

  # Delete the local cluster along with the registry, resolver, mirrors and
  # router, which are kept while another local cluster still uses them:
  educates local cluster delete --all
`

type LocalClusterDeleteOptions struct {
	Kubeconfig    string
	AllComponents bool
}

// Run deletes the kind cluster of the local cluster id. The cluster's
// config stays in the data home.
//
// Once the kind cluster is gone, the CA key pair the cluster issued its
// ingress certificates from is removed from the local secrets cache when
// no other local cluster uses it, as removeUnusedCAKeyPairs decides. A
// later deploy of the kept config mints a new one from the root CA.
//
// With AllComponents it then deletes the host infrastructure every local
// cluster shares, the local registry, the DNS resolver, the registry
// mirrors and the router, with the router folder of the data home but not
// the router file, router.yaml, which holds the router address. It does so
// only when no other Educates cluster remains to use them; otherwise it
// keeps them and names the clusters still using them. It does so even
// when the kind cluster was already gone, and still reports that.
//
// A DNS resolver and a router that keep running are refreshed once the
// kind cluster is gone, so they stop serving its domains. One that can't
// be refreshed is only a warning, since the cluster is gone by then.
func (o *LocalClusterDeleteOptions) Run(ctx context.Context, id localclusters.Identity) error {
	c := cluster.NewKindClusterConfig("", id)

	// The cluster can say which CA key pair it uses only while its kind
	// cluster, and its kube context, exist.
	keyPairs := clusterCAKeyPairs(ctx, id, readLocalClusterConfig, localclusters.LoadConfig)

	err := c.DeleteCluster()
	if !o.AllComponents {
		if err != nil {
			return err
		}
		removeUnusedCAKeyPairs(ctx, os.Stdout, id, keyPairs)
		refreshResolverAfterDelete(ctx)
		refreshRouterAfterDelete(ctx)
		return nil
	}
	if err != nil && !cluster.IsNotFound(err) {
		return err
	}

	removeUnusedCAKeyPairs(ctx, os.Stdout, id, keyPairs)

	inventory, inventoryErr := localClusterInventory(ctx)
	if inventoryErr != nil {
		return inventoryErr
	}
	if users := inventory.SharedInfrastructureUsers(id); len(users) > 0 {
		fmt.Printf("Skipped deleting the local registry, DNS resolver, registry mirrors and router: still in use by %s.\n",
			localclusters.DescribeClusters(users))
		refreshResolverAfterDelete(ctx)
		refreshRouterAfterDelete(ctx)
		return err
	}

	// Best-effort cleanup: surface each as a step but don't fail the
	// delete if a component is already gone.
	_ = stepOnStdout(false, "delete local registry", "deleted", func(s progress.Step) error {
		return registry.DeleteRegistry(s)
	})
	resolver.DeleteResolver()
	_ = stepOnStdout(false, "delete registry mirrors", "deleted", func(s progress.Step) error {
		return registry.DeleteRegistryMirrors(s)
	})
	_ = stepOnStdout(false, "delete router", "deleted", func(progress.Step) error {
		return dockerRouterOperations(utils.GetEducatesHomeDir(), os.Stderr).removeRouter(ctx)
	})

	return err
}

// educatesClusterConfigResource is the EducatesClusterConfig custom
// resource, whose singleton cluster names the CA key pair a local
// cluster issues its ingress certificates from.
var educatesClusterConfigResource = schema.GroupVersionResource{Group: "config.educates.dev", Version: "v1alpha1", Resource: "educatesclusterconfigs"}

// clusterConfigReadTimeout bounds the read of a local cluster's
// EducatesClusterConfig, so a cluster that does not answer delays a
// delete only that long.
const clusterConfigReadTimeout = 5 * time.Second

// readClusterConfigFunc reads the EducatesClusterConfig of the local
// cluster id from its cluster.
type readClusterConfigFunc func(ctx context.Context, id localclusters.Identity) (*unstructured.Unstructured, error)

// loadConfigFunc loads the config of the local cluster id from the data
// home, as localclusters.LoadConfig does.
type loadConfigFunc func(id localclusters.Identity) (localclusters.LoadedConfig, error)

// readLocalClusterConfig reads the EducatesClusterConfig of the local
// cluster id through its kube context, in the kubeconfig file kind writes
// it into, waiting at most clusterConfigReadTimeout.
func readLocalClusterConfig(ctx context.Context, id localclusters.Identity) (*unstructured.Unstructured, error) {
	ctx, cancel := context.WithTimeout(ctx, clusterConfigReadTimeout)
	defer cancel()

	client, err := cluster.NewClusterConfig(cluster.KindKubeconfigPath(""), id.Context).GetDynamicClient()
	if err != nil {
		return nil, err
	}

	return client.Resource(educatesClusterConfigResource).Get(ctx, "cluster", metav1.GetOptions{})
}

// clusterCAKeyPairs returns what the local cluster id uses of the CA key
// pairs in the local secrets cache.
//
// While its cluster answers with an EducatesClusterConfig, that is the CA
// key pair the EducatesClusterConfig references; for a cluster whose TLS
// ends at the router, which references none, the CA key pair of its
// domain, which the router's certificate is issued from; and otherwise
// none, as for a TLS certificate of the user's or plain HTTP.
// When the cluster does not answer, as a stopped one does not, or has no
// EducatesClusterConfig, as one created with --cluster-only has none, it
// is the CA key pair for the ingress domain of the cluster's config in the
// data home. When neither tells, as for a stopped cluster created with
// --config, whose config is not in the data home, it is not known.
func clusterCAKeyPairs(ctx context.Context, id localclusters.Identity, readClusterConfig readClusterConfigFunc, loadConfig loadConfigFunc) localcerts.ClusterCAKeyPairs {
	result := localcerts.ClusterCAKeyPairs{Identity: id}

	clusterConfig, err := readClusterConfig(ctx, id)
	if err == nil {
		result.Known = true
		name, _, _ := unstructured.NestedString(clusterConfig.Object,
			"spec", "ingress", "certificates", "bundledCertManager", "customCA", "caKeyPairSecretRef", "name")
		if name == "" && tlsEndsOutsideCluster(clusterConfig) {
			// The router ends TLS for the cluster, with a certificate
			// issued from the CA key pair of its domain.
			if domain, _, _ := unstructured.NestedString(clusterConfig.Object, "spec", "ingress", "domain"); domain != "" {
				name = secrets.CAKeyPairName(domain)
			}
		}
		if name != "" {
			result.Names = []string{name}
		}
		return result
	}

	clusterReason := "its cluster did not answer"
	if k8serrors.IsNotFound(err) {
		clusterReason = "its cluster has no EducatesClusterConfig"
	}

	loaded, err := loadConfig(id)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		result.Unknown = clusterReason + " and the data home has no config for it"
	case err != nil:
		result.Unknown = clusterReason + " and its config in the data home does not load"
	case loaded.Domain() == "":
		result.Unknown = clusterReason + " and its config declares no ingress domain"
	default:
		result.Known = true
		result.Names = []string{secrets.CAKeyPairName(loaded.Domain())}
	}

	return result
}

// tlsEndsOutsideCluster reports whether the EducatesClusterConfig
// clusterConfig serves https with no certificate of the cluster's own:
// the None certificates provider with ingress.protocol https, which a
// local cluster has when its TLS ends at the router.
func tlsEndsOutsideCluster(clusterConfig *unstructured.Unstructured) bool {
	provider, _, _ := unstructured.NestedString(clusterConfig.Object, "spec", "ingress", "certificates", "provider")
	protocol, _, _ := unstructured.NestedString(clusterConfig.Object, "spec", "ingress", "protocol")
	return provider == "None" && protocol == "https"
}

// removeUnusedCAKeyPairs removes from the local secrets cache the CA key
// pairs the deleted local cluster id used, as deleted says, that no other
// local cluster uses, and prints to w each one it removes and why it
// keeps any, as localcerts.PlanCAKeyPairCleanup decides. The other local
// clusters are those of the inventory, running or stopped; a kept config
// of a deleted cluster is no local cluster. They are asked only when the
// deleted cluster used a CA key pair the cache holds.
//
// The kind cluster is gone by then, so what fails here is a warning, not
// an error of the delete.
func removeUnusedCAKeyPairs(ctx context.Context, w io.Writer, id localclusters.Identity, deleted localcerts.ClusterCAKeyPairs) {
	cached, err := secrets.ListCachedSecretNames()
	if err != nil {
		fmt.Fprintf(w, "Warning: kept the CA key pairs in the local secrets cache: %v\n", err)
		return
	}

	var others []localcerts.ClusterCAKeyPairs
	if used := localcerts.UsedCachedCAKeyPairs(deleted, cached); len(used) > 0 {
		inventory, err := localClusterInventory(ctx)
		if err != nil {
			fmt.Fprintf(w, "Warning: kept CA key pair %s in the local secrets cache: can't list the local clusters that may use it: %v\n",
				secrets.QuoteNames(used), err)
			return
		}
		for _, other := range inventory.Clusters() {
			if other.KindName != id.KindName {
				others = append(others, clusterCAKeyPairs(ctx, other, readLocalClusterConfig, localclusters.LoadConfig))
			}
		}
	}

	cleanup := localcerts.PlanCAKeyPairCleanup(deleted, others, cached)
	for _, name := range cleanup.Remove {
		if err := secrets.RemoveCachedSecret(name); err != nil {
			fmt.Fprintf(w, "Warning: kept CA key pair %q in the local secrets cache: %v\n", name, err)
			continue
		}
		fmt.Fprintf(w, "Removed CA key pair %q from the local secrets cache: no other local cluster uses it.\n", name)
	}
	for _, notice := range cleanup.Notices {
		fmt.Fprintln(w, notice)
	}
}

// refreshRouterAfterDelete refreshes the router once a local cluster's
// kind cluster is gone, so it stops serving the cluster's domains, and
// removes the cluster's certificate and key from the router folder,
// whatever the router's state, warning when it can't. The reload keeps the
// other clusters' open connections.
func refreshRouterAfterDelete(ctx context.Context) {
	rep := progress.New(os.Stdout, 0, isStdoutTTY(os.Stdout))
	dockerRouterOperations(utils.GetEducatesHomeDir(), os.Stderr).refreshAfterDelete(ctx, rep, localClusterInventory)
}

// refreshResolverAfterDelete refreshes a running local DNS resolver once a
// local cluster's kind cluster is gone, so it stops serving the domains
// of a named cluster, warning when it can't.
func refreshResolverAfterDelete(ctx context.Context) {
	rep := progress.New(os.Stdout, 0, isStdoutTTY(os.Stdout))
	refreshLocalResolver(rep, func() ([]byte, []error, error) {
		return observedResolverConfig(ctx)
	})
}

func (p *ProjectInfo) NewLocalClusterDeleteCmd() *cobra.Command {
	var o LocalClusterDeleteOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "delete",
		Short: "Deletes the local Kubernetes cluster",
		Long: `Deletes the kind cluster of the local cluster --local-cluster or
EDUCATES_LOCAL_CLUSTER selects, keeping its config in the data home.

It also removes from the local secrets cache the CA key pair the cluster
issued its ingress certificates from, unless another local cluster,
running or stopped, uses it. The root CA and any TLS certificate you
cached are never removed, and creating the cluster again mints a new CA
key pair from the root CA.

The router, which serves every local cluster, is refreshed so it no
longer routes the cluster's names, keeping the other clusters' open
connections. The certificate and key the router ended the cluster's TLS
with, if any, are removed from the router folder of the data home, even
when the router container is missing.

With --all it also deletes the local image registry, the DNS resolver, the
registry mirrors and the router, with the router folder of the data home,
unless another local cluster still uses them. The router file,
<data-home>/router.yaml, which holds the router address, is kept.`,
		Example: localClusterDeleteExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, err := selectedEducatesCluster(cmd)
			if err != nil {
				return err
			}
			return o.Run(cmd.Context(), id)
		},
	}

	c.Flags().BoolVar(
		&o.AllComponents,
		"all",
		false,
		"also delete the image registry, resolver, registry mirrors and router, unless another local cluster still uses them",
	)
	addLocalClusterFlag(c)

	return c
}
