package localclusters

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
)

// folderFiles returns the paths of the regular files under dir, relative
// to it, with slashes, and their contents.
func folderFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		files[filepath.ToSlash(rel)] = string(content)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestWriteRouterFolderWritesTheRenderedFiles(t *testing.T) {
	dataHome := t.TempDir()
	files := RenderRouter([]RouterRoute{{Cluster: Default(dataHome), IngressDomain: "educates.test", Node: "educates-control-plane"}})

	if err := WriteRouterFolder(dataHome, files); err != nil {
		t.Fatalf("WriteRouterFolder: %v", err)
	}

	got := folderFiles(t, config.LocalRouterDir(dataHome))
	if len(got) != len(files) {
		t.Errorf("router folder holds %q, want the %d rendered files", slices.Sorted(maps.Keys(got)), len(files))
	}
	for _, f := range files {
		if got[f.Name] != string(f.Content) {
			t.Errorf("%s = %q, want %q", f.Name, got[f.Name], f.Content)
		}
		info, err := os.Stat(filepath.Join(config.LocalRouterDir(dataHome), f.Name))
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o644 {
			t.Errorf("%s has mode %v, want 0644, readable by the router container's user", f.Name, mode)
		}
	}
}

// A cluster deleted since the last render leaves nothing behind in the
// router folder.
func TestWriteRouterFolderRemovesTheFilesNoLongerRendered(t *testing.T) {
	dataHome := t.TempDir()
	test2 := namedID(t, "test2", dataHome)
	both := RenderRouter([]RouterRoute{
		{Cluster: Default(dataHome), IngressDomain: "educates.test", Node: "educates-control-plane"},
		{Cluster: test2, IngressDomain: "test2.educates.test", Node: "educates-test2-control-plane"},
	})
	if err := WriteRouterFolder(dataHome, append(both, RouterFile{Name: "certs/test2.educates.test.pem", Content: []byte("x")})); err != nil {
		t.Fatalf("WriteRouterFolder: %v", err)
	}
	one := RenderRouter([]RouterRoute{{Cluster: Default(dataHome), IngressDomain: "educates.test", Node: "educates-control-plane"}})

	if err := WriteRouterFolder(dataHome, one); err != nil {
		t.Fatalf("WriteRouterFolder: %v", err)
	}

	got := folderFiles(t, config.LocalRouterDir(dataHome))
	var want []string
	for _, f := range one {
		want = append(want, f.Name)
	}
	if names := slices.Sorted(maps.Keys(got)); !slices.Equal(names, slices.Sorted(slices.Values(want))) {
		t.Errorf("router folder holds %q, want %q", names, slices.Sorted(slices.Values(want)))
	}
}

func TestRemoveRouterFolderKeepsTheRouterFile(t *testing.T) {
	dataHome := t.TempDir()
	path := routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	if err := WriteRouterFolder(dataHome, RenderRouter(nil)); err != nil {
		t.Fatalf("WriteRouterFolder: %v", err)
	}

	if err := RemoveRouterFolder(dataHome); err != nil {
		t.Fatalf("RemoveRouterFolder: %v", err)
	}

	if _, err := os.Stat(config.LocalRouterDir(dataHome)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("router folder still there: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("router.yaml is gone: %v", err)
	}
	if err := RemoveRouterFolder(dataHome); err != nil {
		t.Errorf("RemoveRouterFolder of a missing folder: %v", err)
	}
}
