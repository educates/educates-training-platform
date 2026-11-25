package localclusters

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
)

// WriteRouterFolder writes files, as RenderRouter renders them, into the
// router folder of the data home dataHome, config.LocalRouterDir,
// creating it, and removes every other file under the folder, so that it
// holds the files rendered alone, and a deleted cluster leaves nothing
// behind.
//
// Each file is written to a new file beside it, which is then renamed
// over it, so the router never reads a file half written. The router
// container mounts the folder rather than each file, so it sees a file
// replaced this way. Files and folders are readable by every user, since
// the router container's user may not be the CLI's, except a file whose
// Mode says otherwise, such as a certificate's private key, readable only
// by the user.
func WriteRouterFolder(dataHome string, files []RouterFile) error {
	dir := config.LocalRouterDir(dataHome)
	keep := map[string]bool{}
	for _, f := range files {
		path := filepath.Join(dir, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create the router folder %s: %w", filepath.Dir(path), err)
		}
		mode := f.Mode
		if mode == 0 {
			mode = 0o644
		}
		if err := writeFileByRename(path, f.Content, mode); err != nil {
			return err
		}
		keep[path] = true
	}
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		// A dot file is a file another command is writing at the same
		// time, before it renames it into place.
		if err != nil || d.IsDir() || keep[path] || strings.HasPrefix(d.Name(), ".") {
			return err
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove %s, which the router no longer serves: %w", path, err)
		}
		return nil
	})
}

// ReadRouterCertificate returns the certificate the router folder of the
// data home dataHome holds for domain, as WriteRouterFolder wrote it from
// RenderRouter's files, or nil when it holds none, its certificate or its
// key missing.
func ReadRouterCertificate(dataHome, domain string) (*RouterCertificate, error) {
	path := filepath.Join(config.LocalRouterDir(dataHome), filepath.FromSlash(routerCertificateFile(domain)))
	certificate, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the router's certificate for %s: %w", domain, err)
	}
	key, err := os.ReadFile(path + routerKeySuffix)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the router's certificate key for %s: %w", domain, err)
	}
	return &RouterCertificate{Domain: domain, Certificate: certificate, Key: key}, nil
}

// RemoveRouterFolder removes the router folder of the data home dataHome,
// config.LocalRouterDir, with everything in it. The router file,
// router.yaml, which holds the router address, is kept. A missing folder
// is no error.
func RemoveRouterFolder(dataHome string) error {
	dir := config.LocalRouterDir(dataHome)
	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the router folder %s: %w", dir, err)
	}
	return nil
}

// writeFileByRename writes data to the file path with the permissions
// perm, by writing a new file beside it and renaming it over path.
func writeFileByRename(path string, data []byte, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
