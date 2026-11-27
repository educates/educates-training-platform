// Package kubeconfig reads the kubeconfig a cluster is reached through,
// keeping only the context the cluster names. The service reaches clusters
// with a bearer token verified against a CA, so the selected context must
// give both, and a context that authenticates any other way is refused.
package kubeconfig

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

// Config is a kubeconfig with the one context a cluster is reached
// through, its cluster and its user, in the form kubeconfig files take.
type Config struct {
	APIVersion     string         `json:"apiVersion"`
	Kind           string         `json:"kind"`
	Clusters       []NamedCluster `json:"clusters"`
	Contexts       []NamedContext `json:"contexts"`
	CurrentContext string         `json:"current-context"`
	Users          []NamedUser    `json:"users"`
}

// NamedCluster is a kubeconfig's cluster entry.
type NamedCluster struct {
	Name    string  `json:"name"`
	Cluster Cluster `json:"cluster"`
}

// Cluster is the API server a context reaches.
type Cluster struct {
	Server string `json:"server"`
	// CertificateAuthorityData is the base64 encoding of the PEM encoded
	// CA certificates the server's certificate is verified against.
	CertificateAuthorityData string `json:"certificate-authority-data"`
	// TLSServerName is the name sent as the TLS server name and that the
	// server's certificate is verified against, in place of the host of
	// Server, as when the server is reached through a gateway that routes
	// by server name or at an address its certificate does not name.
	TLSServerName string `json:"tls-server-name,omitempty"`
}

// NamedContext is a kubeconfig's context entry.
type NamedContext struct {
	Name    string  `json:"name"`
	Context Context `json:"context"`
}

// Context names the cluster and user of a context.
type Context struct {
	Cluster   string `json:"cluster"`
	User      string `json:"user"`
	Namespace string `json:"namespace,omitempty"`
}

// NamedUser is a kubeconfig's user entry.
type NamedUser struct {
	Name string `json:"name"`
	User User   `json:"user"`
}

// User is the identity a context authenticates as.
type User struct {
	Token string `json:"token"`
}

// file is a kubeconfig file as read, each entry's details kept as they are
// so that only the selected context's are checked.
type file struct {
	CurrentContext string `json:"current-context"`
	Clusters       []struct {
		Name    string         `json:"name"`
		Cluster map[string]any `json:"cluster"`
	} `json:"clusters"`
	Contexts []struct {
		Name    string  `json:"name"`
		Context Context `json:"context"`
	} `json:"contexts"`
	Users []struct {
		Name string         `json:"name"`
		User map[string]any `json:"user"`
	} `json:"users"`
}

// Select reads a kubeconfig and returns it reduced to the context named
// context, or to its current context when context is empty, or to its first
// context when it names no current context. Only the selected context is
// checked, and every other entry is ignored. Its cluster must give a server
// and CA data, and may give the server name to verify the server's
// certificate against. Its user must give a token and no other way to
// authenticate.
func Select(data []byte, context string) (*Config, error) {
	var f file
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("the kubeconfig is not valid YAML: %w", err)
	}

	name := context
	if name == "" {
		name = f.CurrentContext
	}
	if name == "" {
		if len(f.Contexts) == 0 {
			return nil, errors.New("the kubeconfig has no context")
		}
		name = f.Contexts[0].Name
	}

	var selected *NamedContext
	for i := range f.Contexts {
		if f.Contexts[i].Name == name {
			selected = &NamedContext{Name: name, Context: f.Contexts[i].Context}
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("the kubeconfig has no context %q", name)
	}

	cluster, err := selectCluster(&f, selected.Context.Cluster)
	if err != nil {
		return nil, fmt.Errorf("context %q: %w", name, err)
	}
	user, err := selectUser(&f, selected.Context.User)
	if err != nil {
		return nil, fmt.Errorf("context %q: %w", name, err)
	}

	return &Config{
		APIVersion:     "v1",
		Kind:           "Config",
		Clusters:       []NamedCluster{*cluster},
		Contexts:       []NamedContext{*selected},
		CurrentContext: name,
		Users:          []NamedUser{*user},
	}, nil
}

func selectCluster(f *file, name string) (*NamedCluster, error) {
	for _, entry := range f.Clusters {
		if entry.Name != name {
			continue
		}
		server, _ := entry.Cluster["server"].(string)
		if server == "" {
			return nil, fmt.Errorf("its cluster %q has no server", name)
		}
		if _, err := url.Parse(server); err != nil {
			return nil, fmt.Errorf("its cluster %q has an invalid server: %w", name, err)
		}
		ca, _ := entry.Cluster["certificate-authority-data"].(string)
		if ca == "" {
			return nil, fmt.Errorf("its cluster %q has no certificate-authority-data", name)
		}
		if _, err := base64.StdEncoding.DecodeString(ca); err != nil {
			return nil, fmt.Errorf("its cluster %q has certificate-authority-data that is not base64: %w", name, err)
		}
		var serverName string
		switch value := entry.Cluster["tls-server-name"].(type) {
		case nil:
		case string:
			serverName = value
		default:
			return nil, fmt.Errorf("its cluster %q has a tls-server-name that is not a string", name)
		}
		return &NamedCluster{Name: name, Cluster: Cluster{
			Server:                   server,
			CertificateAuthorityData: ca,
			TLSServerName:            serverName,
		}}, nil
	}
	return nil, fmt.Errorf("the kubeconfig has no cluster %q", name)
}

// refusedUserFields are the ways to authenticate, other than a token, that
// the service does not support.
var refusedUserFields = []string{
	"exec",
	"auth-provider",
	"client-certificate",
	"client-certificate-data",
	"client-key",
	"client-key-data",
}

func selectUser(f *file, name string) (*NamedUser, error) {
	for _, entry := range f.Users {
		if entry.Name != name {
			continue
		}
		for _, field := range refusedUserFields {
			if _, ok := entry.User[field]; ok {
				return nil, fmt.Errorf("its user %q authenticates with %s, which is not supported: use a token", name, field)
			}
		}
		token, _ := entry.User["token"].(string)
		if token == "" {
			return nil, fmt.Errorf("its user %q has no token", name)
		}
		return &NamedUser{Name: name, User: User{Token: token}}, nil
	}
	return nil, fmt.Errorf("the kubeconfig has no user %q", name)
}

// Local returns the kubeconfig of the cluster the service runs on, reached
// at server with the remote-access token and the CA mounted in its pod, its
// entries named after the cluster.
func Local(name, server string, token, ca []byte) *Config {
	return &Config{
		APIVersion: "v1",
		Kind:       "Config",
		Clusters: []NamedCluster{{Name: name, Cluster: Cluster{
			Server:                   server,
			CertificateAuthorityData: base64.StdEncoding.EncodeToString(bytes.TrimSpace(ca)),
		}}},
		Contexts:       []NamedContext{{Name: name + "-context", Context: Context{Cluster: name, User: name + "-user"}}},
		CurrentContext: name + "-context",
		Users:          []NamedUser{{Name: name + "-user", User: User{Token: strings.TrimSpace(string(token))}}},
	}
}

// RESTConfig returns the client configuration of the kubeconfig's context.
func (c *Config) RESTConfig() (*rest.Config, error) {
	if len(c.Clusters) != 1 || len(c.Users) != 1 {
		return nil, errors.New("the kubeconfig must hold one cluster and one user")
	}
	ca, err := base64.StdEncoding.DecodeString(c.Clusters[0].Cluster.CertificateAuthorityData)
	if err != nil {
		return nil, fmt.Errorf("decoding the certificate-authority-data: %w", err)
	}
	return &rest.Config{
		Host:        c.Clusters[0].Cluster.Server,
		BearerToken: c.Users[0].User.Token,
		TLSClientConfig: rest.TLSClientConfig{
			CAData:     ca,
			ServerName: c.Clusters[0].Cluster.TLSServerName,
		},
	}, nil
}

// YAML returns the kubeconfig as a kubeconfig file.
func (c *Config) YAML() ([]byte, error) {
	return yaml.Marshal(c)
}

// Equal reports whether two kubeconfigs are the same.
func (c *Config) Equal(other *Config) bool {
	if c == nil || other == nil {
		return c == other
	}
	a, errA := c.YAML()
	b, errB := other.YAML()
	return errA == nil && errB == nil && bytes.Equal(a, b)
}
