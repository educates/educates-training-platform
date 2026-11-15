package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"testing"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// clusterConfigWithCAKeyPair is the EducatesClusterConfig of a local
// deploy, in the shape the API server returns it, whose cert-manager
// issues the ingress certificates from the CA key pair name, or which
// serves a TLS certificate of the user's when name is empty.
func clusterConfigWithCAKeyPair(name string) *unstructured.Unstructured {
	certificates := map[string]interface{}{
		"provider": "StaticCertificate",
		"staticCertificate": map[string]interface{}{
			"tlsSecretRef": map[string]interface{}{"name": "a.test-tls", "namespace": "educates-secrets"},
		},
	}
	if name != "" {
		certificates = map[string]interface{}{
			"provider": "BundledCertManager",
			"bundledCertManager": map[string]interface{}{
				"issuerType": "CustomCA",
				"customCA": map[string]interface{}{
					"caCertificateSecretRef": map[string]interface{}{"name": "educates-ca", "namespace": "educates-secrets"},
					"caKeyPairSecretRef":     map[string]interface{}{"name": name, "namespace": "educates-secrets"},
				},
			},
		}
	}
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "config.educates.dev/v1alpha1",
		"kind":       "EducatesClusterConfig",
		"metadata":   map[string]interface{}{"name": "cluster"},
		"spec": map[string]interface{}{
			"ingress": map[string]interface{}{"domain": "a.test", "certificates": certificates},
		},
	}}
}

func clusterAnswers(clusterConfig *unstructured.Unstructured) readClusterConfigFunc {
	return func(context.Context, localclusters.Identity) (*unstructured.Unstructured, error) {
		return clusterConfig, nil
	}
}

func clusterFails(err error) readClusterConfigFunc {
	return func(context.Context, localclusters.Identity) (*unstructured.Unstructured, error) {
		return nil, err
	}
}

func configWithDomain(domain string) loadConfigFunc {
	return func(id localclusters.Identity) (localclusters.LoadedConfig, error) {
		cfg := &v1alpha1.EducatesLocalConfig{}
		cfg.Ingress.Domain = domain
		return localclusters.LoadedConfig{Identity: id, Config: cfg, Source: id.ConfigPath}, nil
	}
}

func configFails(err error) loadConfigFunc {
	return func(localclusters.Identity) (localclusters.LoadedConfig, error) {
		return localclusters.LoadedConfig{}, err
	}
}

func TestClusterCAKeyPairs(t *testing.T) {
	notAnswering := errors.New("dial tcp 127.0.0.1:6443: connect: connection refused")
	noClusterConfig := k8serrors.NewNotFound(educatesClusterConfigResource.GroupResource(), "cluster")
	noConfig := fmt.Errorf("no config: %w", fs.ErrNotExist)

	tests := []struct {
		name        string
		read        readClusterConfigFunc
		load        loadConfigFunc
		wantKnown   bool
		wantNames   []string
		wantUnknown string
	}{
		{
			name:      "the CA key pair the cluster's EducatesClusterConfig references",
			read:      clusterAnswers(clusterConfigWithCAKeyPair("a.test-ca-key-pair")),
			load:      configWithDomain("b.test"),
			wantKnown: true,
			wantNames: []string{"a.test-ca-key-pair"},
		},
		{
			name:      "none when the cluster's EducatesClusterConfig references none",
			read:      clusterAnswers(clusterConfigWithCAKeyPair("")),
			load:      configWithDomain("a.test"),
			wantKnown: true,
		},
		{
			name:      "the CA key pair of the config's domain when the cluster does not answer",
			read:      clusterFails(notAnswering),
			load:      configWithDomain("a.test"),
			wantKnown: true,
			wantNames: []string{"a.test-ca-key-pair"},
		},
		{
			name:      "the CA key pair of the config's domain when the cluster has no EducatesClusterConfig",
			read:      clusterFails(noClusterConfig),
			load:      configWithDomain("a.test"),
			wantKnown: true,
			wantNames: []string{"a.test-ca-key-pair"},
		},
		{
			name:        "unknown when the cluster does not answer and the data home has no config",
			read:        clusterFails(notAnswering),
			load:        configFails(noConfig),
			wantUnknown: "its cluster did not answer and the data home has no config for it",
		},
		{
			name:        "unknown when the cluster has no EducatesClusterConfig and the config does not load",
			read:        clusterFails(noClusterConfig),
			load:        configFails(errors.New("bad yaml")),
			wantUnknown: "its cluster has no EducatesClusterConfig and its config in the data home does not load",
		},
		{
			name:        "unknown when the config declares no ingress domain",
			read:        clusterFails(notAnswering),
			load:        configWithDomain(""),
			wantUnknown: "its cluster did not answer and its config declares no ingress domain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id := localclusters.Default(t.TempDir())

			got := clusterCAKeyPairs(context.Background(), id, tt.read, tt.load)

			if got.Identity != id {
				t.Errorf("Identity = %+v, want %+v", got.Identity, id)
			}
			if got.Known != tt.wantKnown {
				t.Errorf("Known = %v, want %v", got.Known, tt.wantKnown)
			}
			if !slices.Equal(got.Names, tt.wantNames) {
				t.Errorf("Names = %q, want %q", got.Names, tt.wantNames)
			}
			if got.Unknown != tt.wantUnknown {
				t.Errorf("Unknown = %q, want %q", got.Unknown, tt.wantUnknown)
			}
		})
	}
}
