package packages

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	imgpkgcmd "carvel.dev/imgpkg/pkg/imgpkg/cmd"
	"carvel.dev/imgpkg/pkg/imgpkg/registry"
	regname "github.com/google/go-containerregistry/pkg/name"
	"github.com/pkg/errors"
)

// DefaultImageRepository is where a package image is published when no
// repository is given, matching the local registry the laptop flow runs.
const DefaultImageRepository = "localhost:5001"

// PublishOptions are the inputs of the publish command.
type PublishOptions struct {
	// Path is the package source root. Empty means the working directory.
	Path string

	// Image overrides the whole reference and wins over the two below.
	Image string

	// ImageRepository is the repository the reference is derived in.
	ImageRepository string

	// ImageTag overrides the tag only, leaving the derived name alone.
	ImageTag string

	// Platforms names the platforms to build. Empty means all supported ones.
	Platforms []string

	// DryRun assembles and validates without contacting the registry.
	DryRun bool

	// DigestFile is a path to write the index digest to.
	DigestFile string

	RegistryFlags imgpkgcmd.RegistryFlags
}

// Publish assembles the package image and, unless this is a dry run, pushes
// the children and the index.
func (o *PublishOptions) Publish(stdout io.Writer) error {
	sourceDir, err := o.sourceDir()

	if err != nil {
		return err
	}

	platforms, singlePlatform, err := o.platforms()

	if err != nil {
		return err
	}

	timestamp, err := Timestamp(os.Getenv)

	if err != nil {
		return err
	}

	build, err := BuildPackageImage(sourceDir, platforms, timestamp)

	if err != nil {
		return err
	}

	reference, err := o.reference(build.Manifest)

	if err != nil {
		return err
	}

	if !o.DryRun {
		if err := o.push(reference, build); err != nil {
			return err
		}
	}

	if err := o.report(stdout, reference, build, singlePlatform); err != nil {
		return err
	}

	return o.writeDigestFile(build)
}

func (o *PublishOptions) sourceDir() (string, error) {
	path := o.Path

	if path == "" {
		path = "."
	}

	path, err := filepath.Abs(filepath.Clean(path))

	if err != nil {
		return "", errors.Wrap(err, "unable to resolve the package source path")
	}

	info, err := os.Stat(path)

	if err != nil || !info.IsDir() {
		return "", errors.Errorf("the package source %s does not exist or is not a directory", path)
	}

	return path, nil
}

// platforms resolves the requested platforms, reporting whether only one was
// asked for so the caller can warn.
func (o *PublishOptions) platforms() ([]Platform, bool, error) {
	if len(o.Platforms) == 0 {
		return SupportedPlatforms, false, nil
	}

	seen := map[string]bool{}

	var platforms []Platform

	for _, value := range o.Platforms {
		platform, err := ParsePlatform(strings.TrimSpace(value))

		if err != nil {
			return nil, false, err
		}

		if seen[platform.String()] {
			continue
		}

		seen[platform.String()] = true

		platforms = append(platforms, platform)
	}

	return platforms, len(platforms) == 1, nil
}

// reference derives the image reference. The manifest supplies the name and
// version unless a flag overrides them; none of them rewrites the manifest.
func (o *PublishOptions) reference(manifest *Manifest) (regname.Tag, error) {
	value := o.Image

	if value == "" {
		repository := o.ImageRepository

		if repository == "" {
			repository = DefaultImageRepository
		}

		tag := o.ImageTag

		if tag == "" {
			tag = manifest.Version
		}

		value = fmt.Sprintf("%s/%s:%s", strings.TrimSuffix(repository, "/"), manifest.Name, tag)
	}

	reference, err := regname.NewTag(value)

	if err != nil {
		return regname.Tag{}, errors.Wrapf(err, "unable to parse the image reference %q", value)
	}

	return reference, nil
}

func (o *PublishOptions) push(reference regname.Tag, build *Build) error {
	registryOpts := o.RegistryFlags.AsRegistryOpts()

	simpleRegistry, err := registry.NewSimpleRegistry(registryOpts)

	if err != nil {
		return errors.Wrap(err, "unable to reach the image registry")
	}

	// The children are written first so the index never refers to a manifest
	// which is not yet present.
	for _, child := range build.Children {
		digest, err := child.Image.Digest()

		if err != nil {
			return errors.Wrapf(err, "unable to compute the digest for %s", child.Platform)
		}

		childReference := reference.Context().Digest(digest.String())

		if err := simpleRegistry.WriteImage(childReference, child.Image, nil); err != nil {
			return errors.Wrapf(err, "unable to publish the image for %s", child.Platform)
		}
	}

	if err := simpleRegistry.WriteIndex(reference, build.Index); err != nil {
		return errors.Wrap(err, "unable to publish the package image")
	}

	return nil
}

func (o *PublishOptions) report(stdout io.Writer, reference regname.Tag, build *Build, singlePlatform bool) error {
	digest, err := build.Index.Digest()

	if err != nil {
		return errors.Wrap(err, "unable to compute the package image digest")
	}

	if o.DryRun {
		fmt.Fprintf(stdout, "Dry run, nothing was published.\n")
	}

	fmt.Fprintf(stdout, "Package image: %s\n", reference.Name())
	fmt.Fprintf(stdout, "Digest: %s\n", digest.String())

	for _, child := range build.Children {
		childDigest, err := child.Image.Digest()

		if err != nil {
			return errors.Wrapf(err, "unable to compute the digest for %s", child.Platform)
		}

		fmt.Fprintf(stdout, "  %s %s\n", child.Platform, childDigest.String())
	}

	if len(build.Ignored) != 0 {
		fmt.Fprintf(stdout, "Ignored in the package source: %s\n", strings.Join(build.Ignored, ", "))
	}

	if singlePlatform {
		fmt.Fprintf(stdout, "Warning: only one platform was published, so this package image cannot be used on any other architecture.\n")
	}

	return nil
}

func (o *PublishOptions) writeDigestFile(build *Build) error {
	if o.DigestFile == "" {
		return nil
	}

	digest, err := build.Index.Digest()

	if err != nil {
		return errors.Wrap(err, "unable to compute the package image digest")
	}

	if err := os.WriteFile(o.DigestFile, []byte(digest.String()+"\n"), 0o644); err != nil {
		return errors.Wrapf(err, "unable to write the digest to %s", o.DigestFile)
	}

	return nil
}
