package localclusters

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

func TestCheckMirrorsRefusesAMirrorWhoseContainerHasAnotherURL(t *testing.T) {
	dataHome := t.TempDir()
	observed := Observed{
		Nodes: []NodeContainer{controlPlane("educates", "10.10.10.1", true)},
		Mirrors: []MirrorContainer{
			{Mirror: "docker.io", URL: "registry-1.docker.io"},
		},
	}

	err := NewInventory(observed, dataHome).CheckMirrors([]v1alpha1.RegistryMirror{
		{Mirror: "docker.io", URL: "mirror.gcr.io"},
	})

	assertErrorNames(t, err, filepath.Join(dataHome, "mirrors.yaml")+" declares registry mirror docker.io",
		"mirror.gcr.io", "registry-1.docker.io", "every local cluster shares",
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
			wantNames: []string{"mirrors.yaml declares registry mirror docker.io", "username alice", "username bob"},
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

			err := NewInventory(observed, dataHome).CheckMirrors([]v1alpha1.RegistryMirror{tc.declared})

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

	if err := NewInventory(observed, dataHome).CheckMirrors(declared); err != nil {
		t.Errorf("CheckMirrors() = %v, want nil", err)
	}
}

func TestMirrorsToLinkAreEveryMirrorContainerAndEveryDeclaredMirror(t *testing.T) {
	observed := Observed{Mirrors: []MirrorContainer{
		// Created by local mirror deploy, or declared earlier.
		{Mirror: "quay.io"},
		{Mirror: "ghcr.io", URL: "ghcr.example.com", Username: "carol"},
		// Declared too.
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

func TestMirrorsToLinkWhenNoMirrorIsDeclared(t *testing.T) {
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

func TestDeletedMirrorWarningOfADeclaredMirrorNamesTheMirrorsFile(t *testing.T) {
	dataHome := t.TempDir()
	declared := []v1alpha1.RegistryMirror{{Mirror: "quay.io"}, {Mirror: "docker.io", Username: "alice", Password: "secret"}}

	got := NewInventory(Observed{}, dataHome).DeletedMirrorWarning("docker.io", declared)

	for _, want := range []string{filepath.Join(dataHome, "mirrors.yaml") + " declares registry mirror docker.io",
		"'educates local cluster create'", "'educates local cluster start'", "creates the mirror again",
		"Remove the mirror from " + filepath.Join(dataHome, "mirrors.yaml")} {
		if !strings.Contains(got, want) {
			t.Errorf("DeletedMirrorWarning() = %q, want it to mention %q", got, want)
		}
	}
	if strings.Contains(got, "secret") {
		t.Errorf("DeletedMirrorWarning() = %q, gives the mirror's password", got)
	}
}

func TestDeletedMirrorWarningOfAMirrorNotDeclaredIsNone(t *testing.T) {
	declared := []v1alpha1.RegistryMirror{{Mirror: "quay.io"}}

	if got := NewInventory(Observed{}, t.TempDir()).DeletedMirrorWarning("docker.io", declared); got != "" {
		t.Errorf("DeletedMirrorWarning() = %q, want none", got)
	}
}

func TestListMirrorsAreEveryMirrorContainerAndEveryDeclaredMirror(t *testing.T) {
	dockerIO := v1alpha1.RegistryMirror{Mirror: "docker.io", Username: "alice", Password: "secret"}
	gcrIO := v1alpha1.RegistryMirror{Mirror: "gcr.io", URL: "mirror.gcr.io"}
	containers := []string{"quay.io", "docker.io"}
	declared := []v1alpha1.RegistryMirror{dockerIO, gcrIO}

	got := ListMirrors(containers, declared)

	want := []ListedMirror{
		// Declared, with its container.
		{Mirror: "docker.io", Declaration: &dockerIO, HasContainer: true},
		// Declared, with no container yet: the next create or start
		// creates it.
		{Mirror: "gcr.io", Declaration: &gcrIO},
		// Deployed with local mirror deploy, and not declared.
		{Mirror: "quay.io", HasContainer: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ListMirrors() = %+v, want %+v", got, want)
	}
}

func TestListMirrorsOfNoContainerAndNoDeclaredMirrorIsEmpty(t *testing.T) {
	if got := ListMirrors(nil, nil); len(got) != 0 {
		t.Errorf("ListMirrors() = %+v, want none", got)
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
