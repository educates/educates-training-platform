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
