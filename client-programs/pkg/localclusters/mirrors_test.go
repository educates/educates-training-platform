package localclusters

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
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
		"educates local mirror delete docker.io", "educates local mirror deploy docker.io")
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

func TestRemoveMirrorRecordRemovesTheEntryOfAMirror(t *testing.T) {
	quay := v1alpha1.RegistryMirror{Mirror: "quay.io"}
	dockerIO := v1alpha1.RegistryMirror{Mirror: "docker.io", Username: "alice", Password: "secret"}
	cases := []struct {
		name     string
		observed Observed
		want     MirrorRemoval
		wantNote []string
	}{
		{
			name:     "with its container",
			observed: Observed{Mirrors: []MirrorContainer{{Mirror: "docker.io", Username: "alice"}}},
			want:     MirrorRemoval{HasContainer: true},
		},
		{
			name:     "without a container",
			wantNote: []string{"removed registry mirror docker.io from", "mirrors.yaml", "had no container"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			must(t, config.RecordLocalMirror(dataHome, quay))
			must(t, config.RecordLocalMirror(dataHome, dockerIO))

			got, err := NewInventory(tc.observed, dataHome).RemoveMirrorRecord("docker.io")
			if err != nil {
				t.Fatalf("RemoveMirrorRecord: %v", err)
			}

			if got.HasContainer != tc.want.HasContainer {
				t.Errorf("RemoveMirrorRecord().HasContainer = %v, want %v", got.HasContainer, tc.want.HasContainer)
			}
			if len(tc.wantNote) == 0 && got.Note != "" {
				t.Errorf("RemoveMirrorRecord().Note = %q, want none", got.Note)
			}
			for _, want := range tc.wantNote {
				if !strings.Contains(got.Note, want) {
					t.Errorf("RemoveMirrorRecord().Note = %q, want it to mention %q", got.Note, want)
				}
			}
			assertRecordedMirrors(t, dataHome, quay)
		})
	}
}

// A mirror the file doesn't record, deployed before the file recorded
// mirrors, has its container deleted, and the file is left as it is.
func TestRemoveMirrorRecordOfAMirrorWithOnlyAContainer(t *testing.T) {
	dataHome := t.TempDir()
	observed := Observed{Mirrors: []MirrorContainer{{Mirror: "docker.io"}}}

	got, err := NewInventory(observed, dataHome).RemoveMirrorRecord("docker.io")
	if err != nil {
		t.Fatalf("RemoveMirrorRecord: %v", err)
	}

	if want := (MirrorRemoval{HasContainer: true}); got != want {
		t.Errorf("RemoveMirrorRecord() = %+v, want %+v", got, want)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "mirrors.yaml")); !os.IsNotExist(err) {
		t.Errorf("mirrors.yaml was written: %v", err)
	}
}

func TestRemoveMirrorRecordOfAMirrorNeitherRecordedNorDeployedIsAnError(t *testing.T) {
	dataHome := t.TempDir()
	must(t, config.RecordLocalMirror(dataHome, v1alpha1.RegistryMirror{Mirror: "quay.io"}))
	observed := Observed{Mirrors: []MirrorContainer{{Mirror: "ghcr.io"}}}

	_, err := NewInventory(observed, dataHome).RemoveMirrorRecord("docker.io")

	assertErrorNames(t, err, "registry mirror docker.io", "no container",
		filepath.Join(dataHome, "mirrors.yaml")+" does not record it")
	assertRecordedMirrors(t, dataHome, v1alpha1.RegistryMirror{Mirror: "quay.io"})
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

func TestRecordMirrorRecordsAMirrorTheFileDoesNotHave(t *testing.T) {
	dataHome := t.TempDir()
	requested := v1alpha1.RegistryMirror{Mirror: "docker.io", URL: "registry-1.docker.io", Username: "alice", Password: "secret"}

	got, err := NewInventory(Observed{}, dataHome).RecordMirror(requested)
	if err != nil {
		t.Fatalf("RecordMirror: %v", err)
	}

	if want := (MirrorRecord{Mirror: requested}); !reflect.DeepEqual(got, want) {
		t.Errorf("RecordMirror() = %+v, want %+v", got, want)
	}
	assertRecordedMirrors(t, dataHome, requested)
}

// The mirror's container is reused as it is, so one that pulls from
// another remote registry, or as another username, is refused before the
// file records anything.
func TestRecordMirrorRefusesAMirrorWhoseContainerHasOtherValues(t *testing.T) {
	dataHome := t.TempDir()
	observed := Observed{Mirrors: []MirrorContainer{{Mirror: "docker.io", URL: "registry-1.docker.io", Username: "alice"}}}

	_, err := NewInventory(observed, dataHome).RecordMirror(v1alpha1.RegistryMirror{Mirror: "docker.io", Username: "bob"})

	assertErrorNames(t, err, "registry mirror docker.io", "every local cluster shares",
		"URL registry-1.docker.io and username alice", "URL docker.io and username bob",
		"run 'educates local mirror delete docker.io' first")
	if _, err := os.Stat(filepath.Join(dataHome, "mirrors.yaml")); !os.IsNotExist(err) {
		t.Errorf("mirrors.yaml was written for a refused mirror: %v", err)
	}
}

// A mirror whose container matches what is given, which an earlier
// command created, is recorded and deployed with it.
func TestRecordMirrorRecordsAMirrorWhoseContainerMatches(t *testing.T) {
	dataHome := t.TempDir()
	observed := Observed{Mirrors: []MirrorContainer{{Mirror: "docker.io", Username: "alice"}}}
	requested := v1alpha1.RegistryMirror{Mirror: "docker.io", URL: "docker.io", Username: "alice", Password: "secret"}

	if _, err := NewInventory(observed, dataHome).RecordMirror(requested); err != nil {
		t.Fatalf("RecordMirror: %v", err)
	}

	assertRecordedMirrors(t, dataHome, requested)
}

func TestRecordMirrorRefusesAMirrorRecordedWithAnotherURLOrUsername(t *testing.T) {
	recorded := v1alpha1.RegistryMirror{Mirror: "docker.io", URL: "registry-1.docker.io", Username: "alice", Password: "secret"}
	cases := []struct {
		name      string
		requested v1alpha1.RegistryMirror
		wantNames []string
	}{
		{
			name:      "another URL",
			requested: v1alpha1.RegistryMirror{Mirror: "docker.io", URL: "mirror.gcr.io", Username: "alice"},
			wantNames: []string{"URL registry-1.docker.io", "URL mirror.gcr.io"},
		},
		{
			name:      "no URL, which is the mirror's own registry",
			requested: v1alpha1.RegistryMirror{Mirror: "docker.io", Username: "alice"},
			wantNames: []string{"URL registry-1.docker.io", "URL docker.io"},
		},
		{
			name:      "another username",
			requested: v1alpha1.RegistryMirror{Mirror: "docker.io", URL: "registry-1.docker.io", Username: "bob", Password: "hunter2"},
			wantNames: []string{"username alice", "username bob"},
		},
		{
			name:      "no username",
			requested: v1alpha1.RegistryMirror{Mirror: "docker.io", URL: "registry-1.docker.io"},
			wantNames: []string{"username alice", "no username"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			must(t, config.RecordLocalMirror(dataHome, recorded))

			_, err := NewInventory(Observed{}, dataHome).RecordMirror(tc.requested)

			assertErrorNames(t, err, append(tc.wantNames,
				filepath.Join(dataHome, "mirrors.yaml")+" records registry mirror docker.io",
				"run 'educates local mirror delete docker.io' first")...)
			for _, password := range []string{"secret", "hunter2"} {
				if strings.Contains(err.Error(), password) {
					t.Errorf("RecordMirror() error %q gives a password", err)
				}
			}
			assertRecordedMirrors(t, dataHome, recorded)
		})
	}
}

func TestRecordMirrorWithAnotherPasswordReplacesTheRecordedOne(t *testing.T) {
	recorded := v1alpha1.RegistryMirror{Mirror: "docker.io", Username: "alice", Password: "s3cret-old"}
	requested := v1alpha1.RegistryMirror{Mirror: "docker.io", URL: "docker.io", Username: "alice", Password: "s3cret-new"}
	replaced := v1alpha1.RegistryMirror{Mirror: "docker.io", Username: "alice", Password: "s3cret-new"}
	cases := []struct {
		name     string
		observed Observed
		wantNote []string
	}{
		{
			name:     "with the mirror's container",
			observed: Observed{Mirrors: []MirrorContainer{{Mirror: "docker.io", Username: "alice"}}},
			wantNote: []string{"docker.io", "container keeps the password it was created with",
				"'educates local mirror delete docker.io'", "deploy it again"},
		},
		{
			name: "without it, which is created with the new password",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			must(t, config.RecordLocalMirror(dataHome, recorded))

			got, err := NewInventory(tc.observed, dataHome).RecordMirror(requested)
			if err != nil {
				t.Fatalf("RecordMirror: %v", err)
			}

			if !reflect.DeepEqual(got.Mirror, replaced) {
				t.Errorf("RecordMirror().Mirror = %+v, want %+v", got.Mirror, replaced)
			}
			assertRecordedMirrors(t, dataHome, replaced)
			if len(tc.wantNote) == 0 && got.Note != "" {
				t.Errorf("RecordMirror().Note = %q, want none", got.Note)
			}
			for _, want := range tc.wantNote {
				if !strings.Contains(got.Note, want) {
					t.Errorf("RecordMirror().Note = %q, want it to mention %q", got.Note, want)
				}
			}
			for _, password := range []string{"s3cret-old", "s3cret-new"} {
				if strings.Contains(got.Note, password) {
					t.Errorf("RecordMirror().Note = %q gives a password", got.Note)
				}
			}
		})
	}
}

// A mirror recorded with the URL and username given, and no password
// given, is deployed as the file records it, password included, and the
// file is left as it is.
func TestRecordMirrorOfARecordedMirrorWithoutAPasswordKeepsTheRecordedOne(t *testing.T) {
	dataHome := t.TempDir()
	path := filepath.Join(dataHome, "mirrors.yaml")
	const body = "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalMirrors\n" +
		"registryMirrors:\n- mirror: docker.io # Docker Hub\n  username: alice\n  password: secret\n"
	must(t, os.WriteFile(path, []byte(body), 0o600))

	got, err := NewInventory(Observed{}, dataHome).RecordMirror(v1alpha1.RegistryMirror{Mirror: "docker.io", Username: "alice"})
	if err != nil {
		t.Fatalf("RecordMirror: %v", err)
	}

	want := MirrorRecord{Mirror: v1alpha1.RegistryMirror{Mirror: "docker.io", Username: "alice", Password: "secret"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RecordMirror() = %+v, want %+v", got, want)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != body {
		t.Errorf("mirrors.yaml changed to\n%s", data)
	}
}

// must fails the test on err.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// assertRecordedMirrors fails the test unless the registry mirrors file
// of the data home dataHome records exactly the mirrors want.
func assertRecordedMirrors(t *testing.T, dataHome string, want ...v1alpha1.RegistryMirror) {
	t.Helper()
	got, err := config.LoadLocalMirrors(dataHome)
	if err != nil {
		t.Fatalf("LoadLocalMirrors: %v", err)
	}
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mirrors.yaml records %+v, want %+v", got, want)
	}
}

// assertErrorNames fails the test unless err is an error whose message
// contains every one of names.
func assertErrorNames(t *testing.T, err error, names ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want one")
	}
	for _, want := range names {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}
