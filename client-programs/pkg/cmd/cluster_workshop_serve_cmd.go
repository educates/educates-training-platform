package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates"
	educatesResources "github.com/educates/educates-training-platform/client-programs/pkg/educates/resources"
	"github.com/educates/educates-training-platform/client-programs/pkg/renderer"
)

const (
	clusterWorkshopServeExample = `
  # Serve Educates workshop from local system in current workshop directory and using default workshop file
  educates cluster workshop serve

  # Serve Educates workshop from local system with custom workshop file
  educates cluster workshop serve --path ./workshop --workshop-file custom-workshop.yaml

  # Serve Educates workshop from local system with custom workshop file and activate live reload mode
  educates cluster workshop serve --path ./workshop --workshop-file custom-workshop.yaml --patch-workshop
`
)

// TODO: Move this function somewhere and see how to update the code that does this same functionality in many workshop related commands
func calculateWorkshopRoot(path string) (string, error) {
	var err error

	// If path not provided assume the current working directory.

	if path == "" {
		path = "."
	}

	path = filepath.Clean(path)

	if path, err = filepath.Abs(path); err != nil {
		return "", errors.Wrap(err, "couldn't convert workshop directory to absolute path")
	}

	fileInfo, err := os.Stat(path)

	if err != nil || !fileInfo.IsDir() {
		return "", errors.New("workshop directory does not exist or path is not a directory")
	}

	return path, nil
}

type ClusterWorkshopServeOptions struct {
	KubeconfigOptions
	Name            string
	Path            string
	Portal          string
	ProxyProtocol   string
	ProxyHost       string
	ProxyPort       int
	LocalHost       string
	LocalPort       int
	HugoPort        int
	Token           string
	RefreshToken    bool
	Files           bool
	WorkshopFile    string
	WorkshopVersion string
	PatchWorkshop   bool
	DataValuesFlags yttcmd.DataValuesFlags
}

func (o *ClusterWorkshopServeOptions) Run() error {
	var err error

	var name = o.Name
	var path = o.Path
	var portal = o.Portal
	var token = o.Token

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	// Ensure have portal name.
	if portal == "" {
		portal = constants.DefaultPortalName
	}

	// Calculate workshop root and name.
	if path, err = calculateWorkshopRoot(path); err != nil {
		return err
	}

	var workshop *unstructured.Unstructured

	definitionConfig := educates.WorkshopDefinitionConfig{
		Name:            name,
		Path:            path,
		Portal:          portal,
		WorkshopFile:    o.WorkshopFile,
		WorkshopVersion: o.WorkshopVersion,
		DataValueFlags:  o.DataValuesFlags,
	}
	if workshop, err = educates.LoadWorkshopDefinition(&definitionConfig); err != nil {
		return err
	}

	if name == "" {
		name = workshop.GetName()
	}

	// If going to patch hosted workshop, ensure we have an access token.
	if o.PatchWorkshop && token == "" {
		token, err = renderer.GenerateAccessToken(o.RefreshToken)

		if err != nil {
			return errors.Wrap(err, "error generating access token")
		}
	}

	// If patching hosted workshop create an apply the updated configuration.
	if o.PatchWorkshop {
		patchedWorkshop := workshop.DeepCopyObject().(*unstructured.Unstructured)

		proxyDefinition := map[string]interface{}{
			"enabled": true,
			"proxy": map[string]interface{}{
				"protocol":     o.ProxyProtocol,
				"host":         o.ProxyHost,
				"port":         int64(o.ProxyPort),
				"changeOrigin": false,
				"headers": []interface{}{
					map[string]interface{}{
						"name":  "X-Session-Name",
						"value": "$(session_name)",
					},
					map[string]interface{}{
						"name":  "X-Access-Token",
						"value": token,
					},
				},
			},
		}

		unstructured.SetNestedField(patchedWorkshop.Object, proxyDefinition, "spec", "session", "applications", "workshop")

		dynamicClient, err := clusterConfig.GetDynamicClient()

		if err != nil {
			return errors.Wrapf(err, "unable to create Kubernetes client")
		}

		manager := educatesResources.NewWorkshopManager(dynamicClient)
		// Update the workshop resource in the Kubernetes cluster.
		err = manager.UpdateWorkshopResource(&educatesResources.UpdateWorkshopResourceConfig{
			Workshop: patchedWorkshop,
		})

		if err != nil {
			return err
		}

		fmt.Printf("Patched workshop %q.\n", workshop.GetName())
	}

	var cleanupFunc = func() {
		// Do our best to revert workshop configuration and ignore errors.
		clusterConfig := cluster.NewClusterConfig(o.Kubeconfig, o.Context)

		dynamicClient, err := clusterConfig.GetDynamicClient()

		if err == nil {
			// Update the workshop resource in the Kubernetes cluster.
			manager := educatesResources.NewWorkshopManager(dynamicClient)
			err = manager.UpdateWorkshopResource(&educatesResources.UpdateWorkshopResourceConfig{
				Workshop: workshop,
			})
			if err != nil {
				fmt.Printf("Error restoring workshop %q: %v\n", workshop.GetName(), err)
			} else {
				fmt.Printf("Restored workshop %q.\n", workshop.GetName())
			}
		}
	}

	// Run the proxy server and Hugo server.
	return renderer.RunHugoServer(&renderer.RunHugoServerConfig{
		WorkshopRoot: path,
		Kubeconfig:   o.Kubeconfig,
		Context:      o.Context,
		Workshop:     name,
		Portal:       portal,
		LocalHost:    o.LocalHost,
		LocalPort:    o.LocalPort,
		HugoPort:     o.HugoPort,
		Token:        token,
		Files:        o.Files,
		CleanupFunc:  cleanupFunc,
	})
}

func (p *ProjectInfo) NewClusterWorkshopServeCmd() *cobra.Command {
	var o ClusterWorkshopServeOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "serve",
		Short:   "Serve workshop from local system",
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: clusterWorkshopServeExample,
	}

	c.Flags().StringVarP(
		&o.Name,
		"name",
		"n",
		"",
		"name to be used for the workshop definition, generated if not set",
	)
	c.Flags().StringVarP(
		&o.Path,
		"file",
		"f",
		".",
		"path to local workshop directory",
	)
	c.Flags().StringVar(
		&o.Kubeconfig,
		"kubeconfig",
		"",
		"kubeconfig file to use instead of $KUBECONFIG or $HOME/.kube/config",
	)
	c.Flags().StringVar(
		&o.Context,
		"context",
		"",
		"Context to use from Kubeconfig",
	)
	c.Flags().StringVarP(
		&o.Portal,
		"portal",
		"p",
		constants.DefaultPortalName,
		"name of the training portal to lookup the workshop",
	)
	c.Flags().StringVar(
		&o.ProxyProtocol,
		"proxy-protocol",
		"http",
		"protocol by which any remote proxy will be accessed",
	)
	c.Flags().StringVar(
		&o.ProxyHost,
		"proxy-host",
		"loopback.default.svc.$(cluster_domain)",
		"host by which any remote proxy will be accessed",
	)
	c.Flags().IntVar(
		&o.ProxyPort,
		"proxy-port",
		10081,
		"port on which any remote proxy service will listen",
	)
	c.Flags().StringVar(
		&o.LocalHost,
		"local-host",
		"0.0.0.0",
		"host on which the local proxy will be listen",
	)
	c.Flags().IntVar(
		&o.LocalPort,
		"local-port",
		10081,
		"port on which the local proxy will listen",
	)
	c.Flags().IntVar(
		&o.HugoPort,
		"hugo-port",
		1313,
		"port on which the hugo server will listen",
	)
	c.Flags().StringVarP(
		&o.Token,
		"access-token",
		"",
		"",
		"access token for protecting access to server",
	)
	c.Flags().BoolVarP(
		&o.RefreshToken,
		"refresh-token",
		"",
		false,
		"forcibly refreshes the default generated access token",
	)
	c.Flags().BoolVarP(
		&o.Files,
		"allow-files-download",
		"",
		false,
		"enable download of workshop files as tarball",
	)

	c.Flags().StringVar(
		&o.WorkshopFile,
		"workshop-file",
		"resources/workshop.yaml",
		"location of the workshop definition file",
	)

	c.Flags().StringVar(
		&o.WorkshopVersion,
		"workshop-version",
		"latest",
		"version of the workshop being published",
	)

	c.Flags().BoolVarP(
		&o.PatchWorkshop,
		"patch-workshop",
		"",
		false,
		"Patch hosted workshop to proxy sessions to local server ",
	)

	c.Flags().StringArrayVar(
		&o.DataValuesFlags.EnvFromStrings,
		"data-values-env",
		nil,
		"Extract data values (as strings) from prefixed env vars (format: PREFIX for PREFIX_all__key1=str) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.EnvFromYAML,
		"data-values-env-yaml",
		nil,
		"Extract data values (parsed as YAML) from prefixed env vars (format: PREFIX for PREFIX_all__key1=true) (can be specified multiple times)",
	)

	c.Flags().StringArrayVar(
		&o.DataValuesFlags.KVsFromStrings,
		"data-value",
		nil,
		"Set specific data value to given value, as string (format: all.key1.subkey=123) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.KVsFromYAML,
		"data-value-yaml",
		nil,
		"Set specific data value to given value, parsed as YAML (format: all.key1.subkey=true) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.KVsFromFiles,
		"data-value-file",
		nil,
		"Set specific data value to contents of a file (format: [@lib1:]all.key1.subkey={file path, HTTP URL, or '-' (i.e. stdin)}) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.FromFiles,
		"data-values-file",
		nil,
		"Set multiple data values via plain YAML files (format: [@lib1:]{file path, HTTP URL, or '-' (i.e. stdin)}) (can be specified multiple times)",
	)

	return c
}
