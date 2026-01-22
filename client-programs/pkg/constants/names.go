package constants

const (
	EducatesClusterName           = "educates"
	RegistryImageV3               = "docker.io/library/registry:3"
	RegistryConfigTargetPath      = "/etc/distribution/config.yml"
	ClusterNetworkName            = "kind"
	EducatesNetworkName           = "educates"
	EducatesRegistryContainer     = "educates-registry"
	EducatesControlPlaneContainer = "educates-control-plane"
	EducatesRegistryRoleLabel     = "registry"
	EducatesMirrorRoleLabel       = "mirror"
	EducatesAppLabel              = "educates"
	EducatesResolverContainerName = "educates-resolver"
)
