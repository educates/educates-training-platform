package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localResolverDeployExample = `
  # Deploy the local DNS resolver using the laptop config:
  educates local resolver deploy

  # Deploy using a specific config file:
  educates local resolver deploy --config config.yaml

  # Override the ingress domain from the config:
  educates local resolver deploy --domain mydomain.test
`

type LocalResolverDeployOptions struct {
	Config      string
	LocalConfig bool
	Domain      string
}

func (o *LocalResolverDeployOptions) Run() error {
	cfg, err := loadResolverInputs(o.Config)
	if err != nil {
		return err
	}
	domain := cfg.Ingress.Domain
	if o.Domain != "" {
		domain = o.Domain
	}
	return resolver.DeployResolver(domain, cfg.Resolver.TargetAddress, cfg.Resolver.ExtraDomains)
}

func (p *ProjectInfo) NewLocalResolverDeployCmd() *cobra.Command {
	var o LocalResolverDeployOptions

	c := &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "deploy",
		Short:   "Deploys a local DNS resolver (macOS)",
		Example: localResolverDeployExample,
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run() },
	}
	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind)")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false, "use <data-home>/config.yaml (default when --config is not given)")
	c.Flags().StringVar(&o.Domain, "domain", "", "override ingress.domain from the config")
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	return c
}

// loadResolverInputs loads an EducatesLocalConfig from configPath, or from
// <data-home>/config.yaml when configPath is empty, and returns the parts
// the resolver helpers need. EducatesConfig (escape hatch) is accepted
// when target.cluster / target.resolver are populated; its ingress domain
// is the one its educatesClusterConfig section declares.
func loadResolverInputs(configPath string) (*v1alpha1.EducatesLocalConfig, error) {
	path := configPath
	if path == "" {
		path = defaultLocalCluster().ConfigPath
		if err := config.EnsureLocalConfigFile(utils.GetEducatesHomeDir()); err != nil {
			return nil, err
		}
	}
	loaded, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	switch c := loaded.(type) {
	case *v1alpha1.EducatesLocalConfig:
		return c, nil
	case *v1alpha1.EducatesConfig:
		if c.Target == nil {
			return nil, fmt.Errorf("%s: EducatesConfig has no target block; resolver needs target.resolver.*", path)
		}
		return &v1alpha1.EducatesLocalConfig{
			TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.APIVersion, Kind: v1alpha1.KindEducatesLocalConfig},
			Cluster:  c.Target.Cluster,
			Resolver: c.Target.Resolver,
			Ingress:  v1alpha1.LocalIngressConfig{Domain: c.IngressDomain()},
		}, nil
	default:
		return nil, fmt.Errorf("%s: unsupported kind %q for resolver commands", path, loaded.GetKind())
	}
}
