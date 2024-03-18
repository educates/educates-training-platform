package cmd

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/vmware-tanzu-labs/educates-training-platform/client-programs/pkg/cluster"
	"github.com/vmware-tanzu-labs/educates-training-platform/client-programs/pkg/renderer"
	"github.com/vmware-tanzu-labs/educates-training-platform/client-programs/pkg/utils"
)

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

// func calculateWorkshopName(name string, path string, portal string, workshopFile string, workshopVersion string, dataValuesFlags yttcmd.DataValuesFlags) (string, error) {
// 	var err error

// 	if name == "" {
// 		var workshop *unstructured.Unstructured

// 		if workshop, err = loadWorkshopDefinition(name, path, portal, workshopFile, workshopVersion, dataValuesFlags); err != nil {
// 			return "", err
// 		}

// 		name = workshop.GetName()
// 	}

// 	return name, nil
// }

type ClusterWorkshopServeOptions struct {
	Name            string
	Path            string
	Kubeconfig      string
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

func generateAccessToken(refresh bool) (string, error) {
	configFileDir := utils.GetEducatesHomeDir()
	accessTokenFile := path.Join(configFileDir, "live-reload-token.dat")

	err := os.MkdirAll(configFileDir, os.ModePerm)

	if err != nil {
		return "", errors.Wrapf(err, "unable to create config directory")
	}

	var accessToken string

	if refresh {
		accessToken = randomPassword(32)

		err := ioutil.WriteFile(accessTokenFile, []byte(accessToken), 0644)

		if err != nil {
			return "", err
		}
	} else {
		if _, err := os.Stat(accessTokenFile); err == nil {
			accessTokenBytes, err := ioutil.ReadFile(accessTokenFile)

			if err != nil {
				return "", err
			}

			accessToken = string(accessTokenBytes)
		} else if os.IsNotExist(err) {
			accessToken = randomPassword(32)

			err = ioutil.WriteFile(accessTokenFile, []byte(accessToken), 0644)

			if err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	}

	return accessToken, nil
}

func (o *ClusterWorkshopServeOptions) Run() error {
	var err error

	var name = o.Name
	var path = o.Path
	var portal = o.Portal
	var token = o.Token

	// Ensure have portal name.

	if portal == "" {
		portal = "educates-cli"
	}

	// Calculate workshop root and name.

	if path, err = calculateWorkshopRoot(path); err != nil {
		return err
	}

	var workshop *unstructured.Unstructured

	if workshop, err = loadWorkshopDefinition(name, path, portal, o.WorkshopFile, o.WorkshopVersion, o.DataValuesFlags); err != nil {
		return err
	}

	if name == "" {
		name = workshop.GetName()
	}

	// If going to patch hosted workshop, ensure we have an access token.

	if o.PatchWorkshop && token == "" {
		token, err = generateAccessToken(o.RefreshToken)

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

		clusterConfig := cluster.NewClusterConfig(o.Kubeconfig)

		if !cluster.IsClusterAvailable(clusterConfig) {
			return errors.New("Cluster is not available")
		}

		dynamicClient, err := clusterConfig.GetDynamicClient()

		if err != nil {
			return errors.Wrapf(err, "unable to create Kubernetes client")
		}

		// Update the workshop resource in the Kubernetes cluster.

		err = updateWorkshopResource(dynamicClient, patchedWorkshop)

		if err != nil {
			return err
		}

		fmt.Printf("Patched workshop %q.\n", workshop.GetName())
	}

	var cleanupFunc = func() {
		// Do our best to revert workshop configuration and ignore errors.

		clusterConfig := cluster.NewClusterConfig(o.Kubeconfig)

		dynamicClient, err := clusterConfig.GetDynamicClient()

		if err == nil {
			// Update the workshop resource in the Kubernetes cluster.

			updateWorkshopResource(dynamicClient, workshop)

			fmt.Printf("Restored workshop %q.\n", workshop.GetName())
		}
	}

	// Run the proxy server and Hugo server.

	return renderer.RunHugoServer(path, o.Kubeconfig, name, portal, o.LocalHost, o.LocalPort, o.HugoPort, token, o.Files, cleanupFunc)
}

func (p *ProjectInfo) NewClusterWorkshopServeCmd() *cobra.Command {
	var o ClusterWorkshopServeOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "serve",
		Short: "Serve workshop from local system",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
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
	c.Flags().StringVarP(
		&o.Portal,
		"portal",
		"p",
		"educates-cli",
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
