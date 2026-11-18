// Package config is the Lookup Service's configuration: the lookup.educates.dev
// resources in the config namespace, watched through a controller-runtime
// manager and projected into the service's own types.
package config

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
)

// Client is an API client of the service, projected from its ClientConfig.
type Client struct {
	// Name is the name the client logs in with, its ClientConfig's name.
	Name string
	// UID is the uid of the ClientConfig. A ClientConfig created again under
	// the same name is a new client.
	UID string
	// Password is the password the client logs in with.
	Password string
	// User, when set, overrides the user of every session request.
	User string
	// Roles are the roles the client holds.
	Roles []string
	// Tenants are glob patterns naming the tenants the client may act for.
	Tenants []string
}

// HasRole reports whether the client holds the role.
func (c Client) HasRole(role string) bool {
	return slices.Contains(c.Roles, role)
}

// MayActFor reports whether the client may act for the tenant of that
// name, which one of its tenant patterns must match.
func (c Client) MayActFor(tenant string) bool {
	return slices.ContainsFunc(c.Tenants, func(pattern string) bool {
		return globMatches(pattern, tenant)
	})
}

// PasswordMatches reports whether password is the client's password. It
// compares digests in constant time, so the time taken tells nothing about
// the password, its length included.
func (c Client) PasswordMatches(password string) bool {
	expected := sha256.Sum256([]byte(c.Password))
	given := sha256.Sum256([]byte(password))
	return subtle.ConstantTimeCompare(expected[:], given[:]) == 1
}

// Model is the in-memory configuration of the service. It reads through the
// manager's cache, so a request sees the resources as the watch last
// delivered them.
type Model struct {
	reader    client.Reader
	namespace string
}

// NewModel returns the configuration held in namespace, read through reader.
func NewModel(reader client.Reader, namespace string) *Model {
	return &Model{reader: reader, namespace: namespace}
}

// Client returns the client of that name. A ClientConfig being deleted is no
// longer a client.
func (m *Model) Client(ctx context.Context, name string) (Client, bool, error) {
	var config lookupv1beta1.ClientConfig
	err := m.reader.Get(ctx, client.ObjectKey{Namespace: m.namespace, Name: name}, &config)
	if apierrors.IsNotFound(err) {
		return Client{}, false, nil
	}
	if err != nil {
		return Client{}, false, err
	}
	if config.DeletionTimestamp != nil {
		return Client{}, false, nil
	}
	return projectClient(&config), true, nil
}

// Clients returns every client, ordered by name.
func (m *Model) Clients(ctx context.Context) ([]Client, error) {
	var configs lookupv1beta1.ClientConfigList
	if err := m.reader.List(ctx, &configs, client.InNamespace(m.namespace)); err != nil {
		return nil, err
	}
	clients := make([]Client, 0, len(configs.Items))
	for i := range configs.Items {
		if configs.Items[i].DeletionTimestamp != nil {
			continue
		}
		clients = append(clients, projectClient(&configs.Items[i]))
	}
	slices.SortFunc(clients, func(a, b Client) int { return strings.Compare(a.Name, b.Name) })
	return clients, nil
}

// Tenant returns the tenant of that name. A TenantConfig being deleted is
// no longer a tenant.
func (m *Model) Tenant(ctx context.Context, name string) (Tenant, bool, error) {
	var config lookupv1beta1.TenantConfig
	err := m.reader.Get(ctx, client.ObjectKey{Namespace: m.namespace, Name: name}, &config)
	if apierrors.IsNotFound(err) {
		return Tenant{}, false, nil
	}
	if err != nil {
		return Tenant{}, false, err
	}
	if config.DeletionTimestamp != nil {
		return Tenant{}, false, nil
	}
	return projectTenant(&config), true, nil
}

// Tenants returns every tenant, ordered by name.
func (m *Model) Tenants(ctx context.Context) ([]Tenant, error) {
	var configs lookupv1beta1.TenantConfigList
	if err := m.reader.List(ctx, &configs, client.InNamespace(m.namespace)); err != nil {
		return nil, err
	}
	tenants := make([]Tenant, 0, len(configs.Items))
	for i := range configs.Items {
		if configs.Items[i].DeletionTimestamp != nil {
			continue
		}
		tenants = append(tenants, projectTenant(&configs.Items[i]))
	}
	slices.SortFunc(tenants, func(a, b Tenant) int { return strings.Compare(a.Name, b.Name) })
	return tenants, nil
}

// Cluster returns the cluster of that name, with the kubeconfig its Secret
// holds. A ClusterConfig being deleted is no longer a cluster, even while a
// finalizer holds it back.
func (m *Model) Cluster(ctx context.Context, name string) (Cluster, bool, error) {
	var config lookupv1beta1.ClusterConfig
	err := m.reader.Get(ctx, client.ObjectKey{Namespace: m.namespace, Name: name}, &config)
	if apierrors.IsNotFound(err) {
		return Cluster{}, false, nil
	}
	if err != nil {
		return Cluster{}, false, err
	}
	if config.DeletionTimestamp != nil {
		return Cluster{}, false, nil
	}

	var secretErr error
	cluster := projectCluster(&config, func(name string) (map[string][]byte, bool) {
		var secret corev1.Secret
		err := m.reader.Get(ctx, client.ObjectKey{Namespace: m.namespace, Name: name}, &secret)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				secretErr = err
			}
			return nil, false
		}
		return secret.Data, true
	})
	if secretErr != nil {
		return Cluster{}, false, secretErr
	}
	return cluster, true, nil
}

// ClustersUsingSecret returns the names of the clusters whose kubeconfig
// the Secret of that name holds.
func (m *Model) ClustersUsingSecret(ctx context.Context, secret string) ([]string, error) {
	var configs lookupv1beta1.ClusterConfigList
	if err := m.reader.List(ctx, &configs, client.InNamespace(m.namespace)); err != nil {
		return nil, err
	}
	var names []string
	for i := range configs.Items {
		if ref := configs.Items[i].KubeconfigSecretRef(); ref != nil && ref.Name == secret {
			names = append(names, configs.Items[i].Name)
		}
	}
	return names, nil
}

func projectClient(config *lookupv1beta1.ClientConfig) Client {
	return Client{
		Name:     config.Name,
		UID:      string(config.UID),
		Password: config.Spec.Client.Password,
		User:     config.Spec.User,
		Roles:    nonNil(config.Spec.Roles),
		Tenants:  nonNil(config.Spec.Tenants),
	}
}

// nonNil returns values, or an empty list in place of nil, so that a list the
// resource leaves out is written to responses as [] rather than null.
func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return slices.Clone(values)
}
