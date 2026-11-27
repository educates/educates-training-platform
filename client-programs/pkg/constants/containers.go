// Package constants holds shared constant values used across the CLI.
package constants

const (
	// Container label keys applied to the Docker containers Educates
	// creates directly (the local registry, registry mirrors, and the
	// DNS resolver) so they can be discovered, filtered, and cleaned up
	// by label rather than by container name.
	EducatesContainersAppLabelKey      = "educates.dev/app"
	EducatesContainersRoleLabelKey     = "educates.dev/role"
	EducatesContainersMirrorLabelKey   = "educates.dev/mirror"
	EducatesContainersURLLabelKey      = "educates.dev/url"
	EducatesContainersUsernameLabelKey = "educates.dev/username"

	// Container label values.
	EducatesContainersAppLabel          = "educates"
	EducatesContainersRegistryRoleLabel = "registry"
	EducatesContainersMirrorRoleLabel   = "mirror"
	EducatesContainersResolverRoleLabel = "resolver"
	EducatesContainersRouterRoleLabel   = "router"
)
