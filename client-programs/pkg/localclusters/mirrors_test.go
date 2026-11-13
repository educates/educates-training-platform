package localclusters

import (
	"reflect"
	"slices"
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

func TestMirrorsToLinkAreEveryMirrorContainerAndEveryDeclaredMirror(t *testing.T) {
	observed := Observed{Mirrors: []MirrorContainer{
		// Created by another cluster's config or by local mirror deploy.
		{Mirror: "quay.io"},
		{Mirror: "ghcr.io", URL: "ghcr.example.com", Username: "carol"},
		// Declared by this cluster's config too.
		{Mirror: "docker.io", Username: "alice"},
	}}
	declared := []v1alpha1.RegistryMirror{
		{Mirror: "docker.io", Username: "alice", Password: "secret"},
		// No container yet, so it is created from its declaration.
		{Mirror: "gcr.io", URL: "mirror.gcr.io", Username: "dave", Password: "hunter2"},
	}

	got := NewInventory(observed, t.TempDir()).MirrorsToLink(declared)

	want := []v1alpha1.RegistryMirror{
		{Mirror: "docker.io", Username: "alice", Password: "secret"},
		{Mirror: "gcr.io", URL: "mirror.gcr.io", Username: "dave", Password: "hunter2"},
		{Mirror: "ghcr.io", URL: "ghcr.example.com", Username: "carol"},
		{Mirror: "quay.io"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MirrorsToLink() = %+v, want %+v", got, want)
	}
}

func TestMirrorsToLinkOfAClusterThatDeclaresNone(t *testing.T) {
	cases := []struct {
		name     string
		observed []MirrorContainer
		want     []v1alpha1.RegistryMirror
	}{
		{
			name:     "every mirror container is linked",
			observed: []MirrorContainer{{Mirror: "quay.io"}, {Mirror: "docker.io", URL: "registry-1.docker.io"}},
			want:     []v1alpha1.RegistryMirror{{Mirror: "docker.io", URL: "registry-1.docker.io"}, {Mirror: "quay.io"}},
		},
		{
			name: "no mirror container, nothing to link",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewInventory(Observed{Mirrors: tc.observed}, t.TempDir()).MirrorsToLink(nil)

			if len(got) != len(tc.want) || (len(got) > 0 && !reflect.DeepEqual(got, tc.want)) {
				t.Errorf("MirrorsToLink() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestClustersDeclaringMirrorAreTheLocalClustersWhoseConfigDeclaresIt(t *testing.T) {
	dataHome := t.TempDir()
	const dockerIO = "  - mirror: docker.io\n"
	const quayIO = "  - mirror: quay.io\n"
	mirrorsConfig := func(listen string, mirrors ...string) string {
		return localConfigHeader + "cluster:\n  listenAddress: " + listen + "\n  registryMirrors:\n" + strings.Join(mirrors, "")
	}
	writeClusterConfig(t, dataHome, "", mirrorsConfig("10.10.10.1", dockerIO))
	writeClusterConfig(t, dataHome, "test2", mirrorsConfig("10.10.10.2", quayIO, dockerIO))
	writeClusterConfig(t, dataHome, "test3", mirrorsConfig("10.10.10.3", quayIO))
	// A config whose kind cluster doesn't exist.
	writeClusterConfig(t, dataHome, "test4", mirrorsConfig("10.10.10.4", dockerIO))
	// A cluster with no config.
	addClusterFolder(t, dataHome, "test5")
	// A config that doesn't load.
	writeClusterConfig(t, dataHome, "test6", mirrorsConfig("127.0.0.1", dockerIO))
	observed := Observed{Nodes: []NodeContainer{
		controlPlane("educates", "10.10.10.1", true),
		controlPlane("educates-test2", "10.10.10.2", false),
		controlPlane("educates-test3", "10.10.10.3", true),
		controlPlane("educates-test5", "10.10.10.5", true),
		controlPlane("educates-test6", "10.10.10.6", true),
	}}

	got := kindNames(NewInventory(observed, dataHome).ClustersDeclaringMirror("docker.io"))

	want := []string{"educates", "educates-test2"}
	if !slices.Equal(got, want) {
		t.Errorf("ClustersDeclaringMirror(docker.io) = %v, want %v", got, want)
	}
}

func TestClustersDeclaringMirrorOfAMirrorNoConfigDeclares(t *testing.T) {
	dataHome := t.TempDir()
	writeClusterConfig(t, dataHome, "", localConfigHeader+"cluster:\n  listenAddress: 10.10.10.1\n")
	observed := Observed{Nodes: []NodeContainer{controlPlane("educates", "10.10.10.1", true)}}

	if got := NewInventory(observed, dataHome).ClustersDeclaringMirror("docker.io"); len(got) != 0 {
		t.Errorf("ClustersDeclaringMirror(docker.io) = %v, want none", kindNames(got))
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
