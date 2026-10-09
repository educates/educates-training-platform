package packages

import (
	"archive/tar"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	regname "github.com/google/go-containerregistry/pkg/name"
	regv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/pkg/errors"
	"sigs.k8s.io/yaml"
)

// FetchEntry is one package image to place on disk.
type FetchEntry struct {
	// Path is where the package is unpacked to.
	Path string `json:"path"`

	// Image is the package image reference.
	Image string `json:"image"`

	// SecretRef optionally names a secret holding registry credentials, as
	// the workshop definition declared it.
	SecretRef string `json:"secretRef,omitempty"`
}

// FetchConfig is the set of package images a session needs. The renderers
// write it; the fetcher reads it.
type FetchConfig struct {
	APIVersion string       `json:"apiVersion"`
	Kind       string       `json:"kind"`
	Packages   []FetchEntry `json:"packages"`
}

const (
	fetchConfigAPIVersion = "packages.educates.dev/v1alpha1"
	fetchConfigKind       = "PackageFetch"
)

// LoadFetchConfig reads the fetcher's configuration.
func LoadFetchConfig(path string) (*FetchConfig, error) {
	data, err := os.ReadFile(path)

	if err != nil {
		return nil, errors.Wrapf(err, "unable to read %s", path)
	}

	config := &FetchConfig{}

	if err := yaml.UnmarshalStrict(data, config); err != nil {
		return nil, errors.Wrapf(err, "unable to parse %s", path)
	}

	if config.APIVersion != fetchConfigAPIVersion {
		return nil, errors.Errorf("%s must set apiVersion to %s", path, fetchConfigAPIVersion)
	}

	if config.Kind != fetchConfigKind {
		return nil, errors.Errorf("%s must set kind to %s", path, fetchConfigKind)
	}

	for _, entry := range config.Packages {
		if entry.Path == "" || entry.Image == "" {
			return nil, errors.Errorf("every package in %s needs a path and an image", path)
		}

		if err := validatePackagePath(entry.Path); err != nil {
			return nil, errors.Wrapf(err, "unable to use the path in %s", path)
		}
	}

	return config, nil
}

// PackagesRoot is the only directory a package may be unpacked into. Fetching
// replaces the package directory, so the destructive step guards itself here
// rather than relying on whoever wrote the configuration.
const PackagesRoot = "/opt/packages"

// validatePackagePath refuses a path which is not a directory directly under
// the packages root, which is the only shape the platform ever writes.
func validatePackagePath(path string) error {
	cleaned := filepath.Clean(path)

	if !filepath.IsAbs(cleaned) {
		return errors.Errorf("%q is not an absolute path", path)
	}

	parent, name := filepath.Split(cleaned)

	if filepath.Clean(parent) != PackagesRoot || name == "" {
		return errors.Errorf("%q is not a directory directly under %s", path, PackagesRoot)
	}

	return nil
}

// dockerConfig is the shape of a .dockerconfigjson secret payload.
type dockerConfig struct {
	Auths map[string]struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Auth     string `json:"auth"`
	} `json:"auths"`
}

// Keychain resolves registry credentials for the fetcher. It reads the
// Kubernetes Secret documents the session manager mounts, falling back to the
// ambient Docker configuration when a reference is not covered by one.
type Keychain struct {
	// byName holds the credentials of each secret, keyed by the secret name
	// the workshop definition referred to.
	byName map[string]map[string]authn.AuthConfig
}

// LoadKeychain reads every secret document in a directory. A missing
// directory is not an error: a session without private packages has none.
func LoadKeychain(dir string) (*Keychain, error) {
	keychain := &Keychain{byName: map[string]map[string]authn.AuthConfig{}}

	entries, err := os.ReadDir(dir)

	if err != nil {
		if os.IsNotExist(err) {
			return keychain, nil
		}

		return nil, errors.Wrapf(err, "unable to read %s", dir)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		path := filepath.Join(dir, entry.Name())

		data, err := os.ReadFile(path)

		if err != nil {
			return nil, errors.Wrapf(err, "unable to read %s", path)
		}

		name, credentials, err := parseSecret(data)

		if err != nil {
			return nil, errors.Wrapf(err, "unable to read the credentials in %s", path)
		}

		if name != "" {
			keychain.byName[name] = credentials
		}
	}

	return keychain, nil
}

// secretDocument is the part of a Kubernetes Secret the fetcher reads.
type secretDocument struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Type string            `json:"type"`
	Data map[string]string `json:"data"`
}

// parseSecret extracts registry credentials from one Secret document. Both
// the docker configuration form and the plain username and password form
// vendir accepts are supported.
func parseSecret(data []byte) (string, map[string]authn.AuthConfig, error) {
	document := &secretDocument{}

	if err := yaml.Unmarshal(data, document); err != nil {
		return "", nil, errors.Wrap(err, "unable to parse the secret")
	}

	name := document.Metadata.Name
	credentials := map[string]authn.AuthConfig{}

	decode := func(key string) (string, error) {
		value, found := document.Data[key]

		if !found {
			return "", nil
		}

		decoded, err := base64.StdEncoding.DecodeString(value)

		if err != nil {
			return "", errors.Wrapf(err, "unable to decode %s", key)
		}

		return string(decoded), nil
	}

	// A docker configuration secret carries credentials for one or more
	// registry hosts.
	if payload, err := decode(".dockerconfigjson"); err != nil {
		return "", nil, err
	} else if payload != "" {
		config := &dockerConfig{}

		if err := json.Unmarshal([]byte(payload), config); err != nil {
			return "", nil, errors.Wrap(err, "unable to parse the docker configuration")
		}

		for host, auth := range config.Auths {
			entry := authn.AuthConfig{Username: auth.Username, Password: auth.Password}

			if entry.Username == "" && auth.Auth != "" {
				decoded, err := base64.StdEncoding.DecodeString(auth.Auth)

				if err != nil {
					return "", nil, errors.Wrapf(err, "unable to decode the credentials for %s", host)
				}

				if username, password, found := strings.Cut(string(decoded), ":"); found {
					entry.Username = username
					entry.Password = password
				}
			}

			credentials[registryHostKey(host)] = entry
		}

		return name, credentials, nil
	}

	// The plain form names a single host alongside a username and password.
	username, err := decode("username")

	if err != nil {
		return "", nil, err
	}

	password, err := decode("password")

	if err != nil {
		return "", nil, err
	}

	hostname, err := decode("hostname")

	if err != nil {
		return "", nil, err
	}

	if username != "" || password != "" {
		credentials[registryHostKey(hostname)] = authn.AuthConfig{Username: username, Password: password}
	}

	return name, credentials, nil
}

// registryHostKey normalises a host so a reference can be matched against it.
// An empty host matches any reference the secret is named for.
func registryHostKey(host string) string {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")

	return host
}

// Resolve reports the authenticator for a reference, given the secret the
// workshop definition named for it. Where no secret applies the ambient
// Docker configuration is used, which is what a local build relies on.
func (k *Keychain) Resolve(reference regname.Reference, secretRef string) (authn.Authenticator, error) {
	if secretRef != "" {
		credentials, found := k.byName[secretRef]

		if !found {
			return nil, errors.Errorf("no credentials were provided for the secret %q", secretRef)
		}

		host := reference.Context().RegistryStr()

		if entry, found := credentials[host]; found {
			return authn.FromConfig(entry), nil
		}

		// A secret naming no host applies to whatever it was referenced for.
		if entry, found := credentials[""]; found {
			return authn.FromConfig(entry), nil
		}

		return nil, errors.Errorf("the secret %q holds no credentials for %s", secretRef, host)
	}

	return authn.DefaultKeychain.Resolve(reference.Context())
}

// FetchOptions are the inputs of one fetch run.
type FetchOptions struct {
	// Platform is the platform to resolve a package image index for.
	Platform Platform

	// Keychain resolves credentials.
	Keychain *Keychain

	// Insecure allows plain HTTP, for a local registry.
	Insecure bool
}

// Fetch places one package image on disk at its path, replacing whatever is
// there. It resolves an index to the given platform, so a package image needs
// no per-architecture reference.
func Fetch(entry FetchEntry, options FetchOptions, report io.Writer) error {
	reference, err := regname.ParseReference(entry.Image, referenceOptions(options.Insecure)...)

	if err != nil {
		return errors.Wrapf(err, "unable to parse the image reference %q", entry.Image)
	}

	authenticator, err := options.Keychain.Resolve(reference, entry.SecretRef)

	if err != nil {
		return err
	}

	image, err := remote.Image(reference,
		remote.WithAuth(authenticator),
		remote.WithPlatform(regv1.Platform{
			OS:           options.Platform.OS,
			Architecture: options.Platform.Architecture,
		}),
	)

	if err != nil {
		return errors.Wrapf(err, "unable to fetch the package image %s", entry.Image)
	}

	if err := extract(image, entry.Path); err != nil {
		return errors.Wrapf(err, "unable to unpack the package image %s", entry.Image)
	}

	if report != nil {
		digest, err := image.Digest()

		if err == nil {
			_, _ = io.WriteString(report, "Fetched "+entry.Image+" ("+digest.String()+") for "+options.Platform.String()+" into "+entry.Path+"\n")
		}
	}

	return nil
}

func referenceOptions(insecure bool) []regname.Option {
	if insecure {
		return []regname.Option{regname.Insecure}
	}

	return nil
}

// extract unpacks the flattened filesystem of an image into a directory,
// preserving the modes the package image contract normalised at publish.
func extract(image regv1.Image, target string) error {
	if err := os.RemoveAll(target); err != nil {
		return errors.Wrapf(err, "unable to clear %s", target)
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		return errors.Wrapf(err, "unable to create %s", target)
	}

	reader := mutate.Extract(image)

	defer reader.Close()

	tarReader := tar.NewReader(reader)

	// Directory modes are applied after every entry is written, since a
	// read-only directory would otherwise refuse its own contents.
	directoryModes := map[string]os.FileMode{}

	for {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return errors.Wrap(err, "unable to read the package image contents")
		}

		path, err := securePath(target, header.Name)

		if err != nil {
			return err
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return errors.Wrapf(err, "unable to create %s", path)
			}

			directoryModes[path] = header.FileInfo().Mode().Perm()

		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return errors.Wrapf(err, "unable to create the directory for %s", path)
			}

			if err := writeFile(path, tarReader, header.FileInfo().Mode().Perm()); err != nil {
				return err
			}

		default:
			// The contract refuses everything else at publish, so an image
			// carrying one was not built to it.
			return errors.Errorf("the package image contains %s, which is not a file or a directory", header.Name)
		}
	}

	paths := make([]string, 0, len(directoryModes))

	for path := range directoryModes {
		paths = append(paths, path)
	}

	// Deepest first, so a parent is never made read-only before its children
	// are written.
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))

	for _, path := range paths {
		if err := os.Chmod(path, directoryModes[path]); err != nil {
			return errors.Wrapf(err, "unable to set the mode of %s", path)
		}
	}

	return nil
}

func writeFile(path string, contents io.Reader, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)

	if err != nil {
		return errors.Wrapf(err, "unable to create %s", path)
	}

	defer file.Close()

	if _, err := io.Copy(file, contents); err != nil {
		return errors.Wrapf(err, "unable to write %s", path)
	}

	// The mode is set explicitly because the process umask masks the mode
	// given to OpenFile, which would drop the group and other bits the
	// contract normalised.
	if err := file.Chmod(mode); err != nil {
		return errors.Wrapf(err, "unable to set the mode of %s", path)
	}

	return nil
}

// securePath rejects an entry which would escape the target directory.
func securePath(target string, name string) (string, error) {
	cleaned := filepath.Clean(filepath.Join(target, filepath.FromSlash(name)))

	if cleaned != target && !strings.HasPrefix(cleaned, target+string(os.PathSeparator)) {
		return "", errors.Errorf("the package image contains %s, which would be written outside the package directory", name)
	}

	return cleaned, nil
}
