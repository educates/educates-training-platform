package constants

const (
	EducatesClusterName           = "educates"
	RegistryImageV3               = "docker.io/library/registry:3"
	RegistryConfigTargetPath      = "/etc/distribution/config.yml"
	ClusterNetworkName            = "kind"
	EducatesNetworkName           = "educates"
	EducatesRegistryContainer     = "educates-registry"
	EducatesControlPlaneContainer = "educates-control-plane"
	EducatesResolverContainerName = "educates-resolver"

	// Workshop API Group and Version
	EducatesTrainingAPIGroup   = "training.educates.dev"
	EducatesTrainingAPIVersion = "v1beta1"
	EducatesTrainingAPIGroupVersion = "training.educates.dev/v1beta1"

	// Workshop Pod Label/Annotations Keys
	EducatesWorkshopLabelAnnotationURL             = "training.educates.dev/url"
	EducatesWorkshopLabelAnnotationSource          = "training.educates.dev/source"
	EducatesWorkshopLabelAnnotationSession         = "training.educates.dev/session"
	EducatesWorkshopLabelAnnotationWorkshop        = "training.educates.dev/workshop"
	EducatesWorkshopLabelAnnotationComponent       = "training.educates.dev/component"
	EducatesWorkshopLabelAnnotationComponentPortal = "training.educates.dev/component=portal"

	EducatesTrainingLabelAnnotationDomain          = "training.educates.dev/domain"
	EducatesTrainingLabelAnnotationEnvironmentName = "training.educates.dev/environment.name"
	EducatesTrainingLabelAnnotationPortalName      = "training.educates.dev/portal.name"

	// Container Label Keys
	EducatesContainersAppLabelKey       = "educates.dev/app"
	EducatesContainersRoleLabelKey      = "educates.dev/role"
	EducatesContainersMirrorLabelKey    = "educates.dev/mirror"
	EducatesContainersURLLabelKey       = "educates.dev/url"
	EducatesContainersUsernameLabelKey  = "educates.dev/username"
	// Container Label Values
	EducatesContainersRegistryRoleLabel     = "registry"
	EducatesContainersMirrorRoleLabel       = "mirror"
	EducatesContainersResolverRoleLabel     = "resolver"
	EducatesContainersWorkshopRoleLabel     = "workshop"
	EducatesContainersAppLabel              = "educates"
)
