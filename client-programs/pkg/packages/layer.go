package packages

import (
	"archive/tar"
	"bytes"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"
)

// CommonDirName holds the files every platform child shares.
const CommonDirName = "common"

// Platform is one of the platforms a package image is built for. The contract
// reserves exactly these two.
type Platform struct {
	OS           string
	Architecture string
}

// SupportedPlatforms are the only platforms a package image may be built for.
var SupportedPlatforms = []Platform{
	{OS: "linux", Architecture: "amd64"},
	{OS: "linux", Architecture: "arm64"},
}

func (p Platform) String() string {
	return p.OS + "/" + p.Architecture
}

// DirName is the reserved source directory holding this platform's files.
func (p Platform) DirName() string {
	return p.OS + "-" + p.Architecture
}

// ParsePlatform accepts only the reserved platform names.
func ParsePlatform(value string) (Platform, error) {
	for _, platform := range SupportedPlatforms {
		if platform.String() == value {
			return platform, nil
		}
	}

	names := make([]string, 0, len(SupportedPlatforms))

	for _, platform := range SupportedPlatforms {
		names = append(names, platform.String())
	}

	return Platform{}, errors.Errorf("platform %q is not supported, expected one of %s", value, strings.Join(names, ", "))
}

// defaultTimestamp is the fixed modification time used for every entry when
// SOURCE_DATE_EPOCH is not set, so that two builds of the same source produce
// the same digests. It must be positive: a negative epoch makes tar skip
// entries and yields an empty layer.
var defaultTimestamp = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// Timestamp returns the fixed time to stamp on every entry, honouring the
// reproducible builds convention when SOURCE_DATE_EPOCH is set.
func Timestamp(environ func(string) string) (time.Time, error) {
	value := environ("SOURCE_DATE_EPOCH")

	if value == "" {
		return defaultTimestamp, nil
	}

	seconds, err := strconv.ParseInt(value, 10, 64)

	if err != nil {
		return time.Time{}, errors.Wrapf(err, "unable to parse SOURCE_DATE_EPOCH %q", value)
	}

	if seconds <= 0 {
		return time.Time{}, errors.Errorf("SOURCE_DATE_EPOCH must be a positive number of seconds, got %q", value)
	}

	return time.Unix(seconds, 0).UTC(), nil
}

// sourceEntry is one file destined for a child image, keyed by the path it
// will occupy in the image.
type sourceEntry struct {
	imagePath  string
	sourcePath string
	mode       os.FileMode
	isDir      bool
}

// collectEntries overlays the common directory with a platform directory,
// platform winning on conflict, and reports every entry which the contract
// forbids.
func collectEntries(sourceDir string, platform Platform) ([]sourceEntry, error) {
	entries := map[string]sourceEntry{}

	var rejected []string

	for _, dirName := range []string{CommonDirName, platform.DirName()} {
		root := filepath.Join(sourceDir, dirName)

		info, err := os.Stat(root)

		if err != nil {
			if os.IsNotExist(err) {
				// An arch-independent package has no platform directory, and a
				// package whose files are all per-platform has no common one.
				continue
			}

			return nil, errors.Wrapf(err, "unable to read %s", root)
		}

		if !info.IsDir() {
			return nil, errors.Errorf("%s in the package source is not a directory", dirName)
		}

		walkErr := filepath.Walk(root, func(sourcePath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if sourcePath == root {
				return nil
			}

			relative, err := filepath.Rel(root, sourcePath)

			if err != nil {
				return err
			}

			imagePath := filepath.ToSlash(relative)

			if isIgnoredFile(path.Base(imagePath)) {
				if info.IsDir() {
					return filepath.SkipDir
				}

				return nil
			}

			mode := info.Mode()

			switch {
			case mode.IsDir():
				entries[imagePath] = sourceEntry{imagePath: imagePath, sourcePath: sourcePath, isDir: true}
			case mode.IsRegular():
				// A hard link is an ordinary file as far as the mode is
				// concerned, so it is caught by its link count.
				if isHardLink(info) {
					rejected = append(rejected, filepath.ToSlash(filepath.Join(dirName, relative))+" is a hard link")
					return nil
				}

				entries[imagePath] = sourceEntry{imagePath: imagePath, sourcePath: sourcePath, mode: mode}
			default:
				// Symlinks, devices, sockets and named pipes cannot survive
				// both deliveries, so they are rejected rather than dropped.
				rejected = append(rejected, describeRejected(dirName, relative, mode))
			}

			return nil
		})

		if walkErr != nil {
			return nil, errors.Wrapf(walkErr, "unable to read the %s directory of the package source", dirName)
		}
	}

	if len(rejected) != 0 {
		sort.Strings(rejected)

		return nil, errors.Errorf("the package source contains entries which cannot be published: %s", strings.Join(rejected, ", "))
	}

	ordered := make([]sourceEntry, 0, len(entries))

	for _, entry := range entries {
		ordered = append(ordered, entry)
	}

	// A stable order keeps the layer digest reproducible.
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].imagePath < ordered[j].imagePath })

	return ordered, nil
}

// isHardLink reports a regular file with more than one directory entry
// pointing at it. Such a file would be published as an independent copy under
// an image mount but silently dropped by a vendir fetch, so the contract
// refuses it. Where the link count is not available the file is accepted,
// which is the behaviour on a filesystem that does not report one.
func isHardLink(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)

	if !ok {
		return false
	}

	return stat.Nlink > 1
}

func describeRejected(dirName string, relative string, mode os.FileMode) string {
	kind := "an unsupported file type"

	switch {
	case mode&os.ModeSymlink != 0:
		kind = "a symbolic link"
	case mode&os.ModeDevice != 0:
		kind = "a device"
	case mode&os.ModeNamedPipe != 0:
		kind = "a named pipe"
	case mode&os.ModeSocket != 0:
		kind = "a socket"
	}

	return filepath.ToSlash(filepath.Join(dirName, relative)) + " is " + kind
}

// isIgnoredFile reports the macOS artefacts which are always skipped. Nothing
// else starting with a dot is skipped, so a package may ship dotfiles.
func isIgnoredFile(name string) bool {
	return name == ".DS_Store" || strings.HasPrefix(name, "._")
}

// BuildLayer writes the tar for one platform child. Ownership is root, modes
// are normalised, and every entry carries the same fixed timestamp.
func BuildLayer(sourceDir string, platform Platform, manifest []byte, timestamp time.Time) ([]byte, error) {
	entries, err := collectEntries(sourceDir, platform)

	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, errors.Errorf("the package source has no files for %s, only the manifest would be published", platform)
	}

	buffer := &bytes.Buffer{}
	writer := tar.NewWriter(buffer)

	for _, entry := range entries {
		header := &tar.Header{
			Name:    entry.imagePath,
			ModTime: timestamp,
			Uid:     0,
			Gid:     0,
			Uname:   "root",
			Gname:   "root",
			Format:  tar.FormatPAX,
		}

		if entry.isDir {
			header.Typeflag = tar.TypeDir
			header.Name = entry.imagePath + "/"
			header.Mode = 0o755

			if err := writer.WriteHeader(header); err != nil {
				return nil, errors.Wrapf(err, "unable to write the directory %s", entry.imagePath)
			}

			continue
		}

		contents, err := os.ReadFile(entry.sourcePath)

		if err != nil {
			return nil, errors.Wrapf(err, "unable to read %s", entry.sourcePath)
		}

		header.Typeflag = tar.TypeReg
		header.Size = int64(len(contents))

		// Any execute bit in the source makes the file executable for
		// everyone; otherwise it is readable for everyone.
		if entry.mode&0o111 != 0 {
			header.Mode = 0o755
		} else {
			header.Mode = 0o644
		}

		if err := writer.WriteHeader(header); err != nil {
			return nil, errors.Wrapf(err, "unable to write the header for %s", entry.imagePath)
		}

		if _, err := writer.Write(contents); err != nil {
			return nil, errors.Wrapf(err, "unable to write %s", entry.imagePath)
		}
	}

	// The manifest is copied to the image root under both deliveries.
	manifestHeader := &tar.Header{
		Name:     ManifestFileName,
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len(manifest)),
		ModTime:  timestamp,
		Uid:      0,
		Gid:      0,
		Uname:    "root",
		Gname:    "root",
		Format:   tar.FormatPAX,
	}

	if err := writer.WriteHeader(manifestHeader); err != nil {
		return nil, errors.Wrapf(err, "unable to write the header for %s", ManifestFileName)
	}

	if _, err := writer.Write(manifest); err != nil {
		return nil, errors.Wrapf(err, "unable to write %s", ManifestFileName)
	}

	if err := writer.Close(); err != nil {
		return nil, errors.Wrap(err, "unable to complete the package layer")
	}

	return buffer.Bytes(), nil
}

// IgnoredRootEntries lists what was found beside the reserved directories and
// left out of the image, so a publish can say what it did not ship.
func IgnoredRootEntries(sourceDir string) ([]string, error) {
	items, err := os.ReadDir(sourceDir)

	if err != nil {
		return nil, errors.Wrapf(err, "unable to read %s", sourceDir)
	}

	reserved := map[string]bool{CommonDirName: true, ManifestFileName: true}

	for _, platform := range SupportedPlatforms {
		reserved[platform.DirName()] = true
	}

	var ignored []string

	for _, item := range items {
		if reserved[item.Name()] || isIgnoredFile(item.Name()) {
			continue
		}

		ignored = append(ignored, item.Name())
	}

	sort.Strings(ignored)

	return ignored, nil
}
