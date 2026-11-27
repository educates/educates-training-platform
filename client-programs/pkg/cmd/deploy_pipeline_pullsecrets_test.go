package cmd

import (
	"reflect"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// TestPullSecretsToCheck checks that a deploy checks the operator
// namespace for the image pull Secrets of the kinds whose Secrets it does
// not sync, and not for the local kind's, which it syncs from the cache.
func TestPullSecretsToCheck(t *testing.T) {
	names := []string{"mirror"}
	local := &v1alpha1.EducatesLocalConfig{}
	local.ImageRegistry.PullSecrets = names
	gke := &v1alpha1.EducatesGKEConfig{}
	gke.ImageRegistry.PullSecrets = names
	eks := &v1alpha1.EducatesEKSConfig{}
	eks.ImageRegistry.PullSecrets = names
	inline := &v1alpha1.EducatesInlineConfig{}
	inline.ImageRegistry.PullSecrets = names

	for _, c := range []struct {
		cfg  v1alpha1.Config
		want []string
	}{
		{local, nil},
		{gke, names},
		{eks, names},
		{inline, names},
		{&v1alpha1.EducatesConfig{}, nil},
	} {
		if got := pullSecretsToCheck(c.cfg); !reflect.DeepEqual(got, c.want) {
			t.Errorf("pullSecretsToCheck(%T) = %v, want %v", c.cfg, got, c.want)
		}
	}
}
