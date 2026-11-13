package localclusters

import (
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

func TestCheckMirrorsRefusesAMirrorWhoseContainerHasAnotherURL(t *testing.T) {
	dataHome := t.TempDir()
	addClusterFolder(t, dataHome, "test2")
	test2, err := Named("test2", dataHome)
	if err != nil {
		t.Fatal(err)
	}
	observed := Observed{
		Nodes: []NodeContainer{controlPlane("educates", "10.10.10.1", true)},
		Mirrors: []MirrorContainer{
			{Mirror: "docker.io", URL: "registry-1.docker.io"},
		},
	}

	err = NewInventory(observed, dataHome).CheckMirrors(test2, []v1alpha1.RegistryMirror{
		{Mirror: "docker.io", URL: "mirror.gcr.io"},
	})

	assertErrorNames(t, err, `local cluster "test2"`, "docker.io", "mirror.gcr.io", "registry-1.docker.io",
		"educates local mirror delete docker.io")
}

func TestCheckMirrorsRefusesAMirrorWhoseContainerHasAnotherUsername(t *testing.T) {
	dataHome := t.TempDir()
	cases := []struct {
		name      string
		container MirrorContainer
		declared  v1alpha1.RegistryMirror
		wantNames []string
	}{
		{
			name:      "different usernames",
			container: MirrorContainer{Mirror: "docker.io", Username: "alice"},
			declared:  v1alpha1.RegistryMirror{Mirror: "docker.io", Username: "bob", Password: "secret"},
			wantNames: []string{"the default local cluster", "docker.io", "username alice", "username bob"},
		},
		{
			name:      "username declared, container anonymous",
			container: MirrorContainer{Mirror: "docker.io"},
			declared:  v1alpha1.RegistryMirror{Mirror: "docker.io", Username: "bob"},
			wantNames: []string{"docker.io", "username bob", "no username"},
		},
		{
			name:      "container with a username, none declared",
			container: MirrorContainer{Mirror: "docker.io", Username: "alice"},
			declared:  v1alpha1.RegistryMirror{Mirror: "docker.io"},
			wantNames: []string{"docker.io", "username alice", "no username"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observed := Observed{Mirrors: []MirrorContainer{tc.container}}

			err := NewInventory(observed, dataHome).CheckMirrors(Default(dataHome), []v1alpha1.RegistryMirror{tc.declared})

			assertErrorNames(t, err, tc.wantNames...)
		})
	}
}

func TestCheckMirrorsAcceptsMirrorsWhoseContainersMatchOrAreMissing(t *testing.T) {
	dataHome := t.TempDir()
	observed := Observed{Mirrors: []MirrorContainer{
		{Mirror: "ghcr.io"},
		{Mirror: "docker.io", URL: "registry-1.docker.io", Username: "alice"},
		{Mirror: "quay.io", URL: "quay.io"},
		{Mirror: "registry.k8s.io", URL: "elsewhere.example.com", Username: "carol"},
	}}
	declared := []v1alpha1.RegistryMirror{
		// The same values as the container.
		{Mirror: "ghcr.io"},
		// The same values, with a password the container can't show.
		{Mirror: "docker.io", URL: "registry-1.docker.io", Username: "alice", Password: "secret"},
		// No URL, which means the registry the mirror names, as the
		// container's URL does.
		{Mirror: "quay.io"},
		// No container yet, so create makes one with these values.
		{Mirror: "gcr.io", URL: "mirror.gcr.io", Username: "dave"},
	}

	if err := NewInventory(observed, dataHome).CheckMirrors(Default(dataHome), declared); err != nil {
		t.Errorf("CheckMirrors() = %v, want nil", err)
	}
}

// assertErrorNames fails the test unless err is an error whose message
// contains every one of names.
func assertErrorNames(t *testing.T, err error, names ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("CheckMirrors() = nil, want an error")
	}
	for _, want := range names {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("CheckMirrors() error %q does not name %q", err, want)
		}
	}
}
