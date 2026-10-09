package cmd

import (
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/packages"
	"github.com/spf13/cobra"
)

var (
	packagePublishExample = `
  # Publish an extension package from the current directory to the local registry
  educates package publish

  # Publish an extension package from a specific directory
  educates package publish ./packages/argocd

  # Publish an extension package to a specific registry
  educates package publish --image-repository ghcr.io/myorg

  # Publish an extension package with a specific tag
  educates package publish --image-tag sha-5f9081f

  # Validate an extension package without publishing it
  educates package publish --dry-run

  # Publish an extension package for a single platform
  educates package publish --platform linux/arm64

  # Publish an extension package and record the digest for a later step
  educates package publish --digest-file digest.txt
`
)

func (p *ProjectInfo) NewPackagePublishCmd() *cobra.Command {
	var o packages.PublishOptions

	var c = &cobra.Command{
		Args:    maximumArgs(1, "expected at most one PATH argument", "[PATH]"),
		Use:     "publish [PATH]",
		Short:   "Publish an extension package to an image repository",
		Example: packagePublishExample,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				o.Path = args[0]
			}

			return o.Publish(cmd.OutOrStdout())
		},
	}

	c.Flags().StringVar(
		&o.Image,
		"image",
		"",
		"the full reference to publish the extension package as, overriding the repository and tag",
	)

	c.Flags().StringVar(
		&o.ImageRepository,
		"image-repository",
		packages.DefaultImageRepository,
		"the address of the image repository",
	)

	c.Flags().StringVar(
		&o.ImageTag,
		"image-tag",
		"",
		"the tag to publish under, defaulting to the version from the package manifest",
	)

	c.Flags().StringArrayVar(
		&o.Platforms,
		"platform",
		[]string{},
		"platform to build the extension package for, repeatable, defaulting to all supported platforms",
	)

	c.Flags().BoolVar(
		&o.DryRun,
		"dry-run",
		false,
		"validate and assemble the extension package without publishing it",
	)

	c.Flags().StringVar(
		&o.DigestFile,
		"digest-file",
		"",
		"path to write the digest of the published extension package to",
	)

	c.Flags().StringSliceVar(
		&o.RegistryFlags.CACertPaths,
		"registry-ca-cert-path",
		nil,
		"Add CA certificates for registry API",
	)

	c.Flags().BoolVar(
		&o.RegistryFlags.VerifyCerts,
		"registry-verify-certs",
		true,
		"Set whether to verify server's certificate chain and host name",
	)

	c.Flags().BoolVar(
		&o.RegistryFlags.Insecure,
		"registry-insecure",
		false,
		"Allow the use of http when interacting with registries",
	)

	c.Flags().StringVar(
		&o.RegistryFlags.Username,
		"registry-username",
		"",
		"Set username for registry authentication",
	)

	c.Flags().StringVar(
		&o.RegistryFlags.Password,
		"registry-password",
		"",
		"Set password for registry authentication",
	)

	c.Flags().StringVar(
		&o.RegistryFlags.Token,
		"registry-token",
		"",
		"Set token for registry authentication",
	)

	c.Flags().BoolVar(
		&o.RegistryFlags.Anon,
		"registry-anon",
		false,
		"Set anonymous registry authentication",
	)

	c.Flags().DurationVar(
		&o.RegistryFlags.ResponseHeaderTimeout,
		"registry-response-header-timeout",
		30*time.Second,
		"Maximum time to allow a request to wait for a server's response headers from the registry",
	)

	c.Flags().IntVar(
		&o.RegistryFlags.RetryCount,
		"registry-retry-count",
		5,
		"Set the number of times imgpkg retries to send requests to the registry in case of an error",
	)

	return c
}
