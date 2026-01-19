package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/maphash"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type ClusterWorkshopDeployOptions struct {
	KubeconfigOptions
	Name            string
	Alias           string
	Path            string
	Portal          string
	Capacity        uint
	Reserved        uint
	Initial         uint
	Expires         string
	Overtime        string
	Deadline        string
	Orphaned        string
	Overdue         string
	Refresh         string
	Repository      string
	Environ         []string
	Labels          []string
	WorkshopFile    string
	WorkshopVersion string
	OpenBrowser     bool
	DataValuesFlags yttcmd.DataValuesFlags
}

func (o *ClusterWorkshopDeployOptions) Run() error {
	var err error

	var path = o.Path

	// Ensure have portal name.

	if o.Portal == "" {
		o.Portal = constants.DefaultPortalName
	}

	// If path not provided assume the current working directory. When loading
	// the workshop will then expect the workshop definition to reside in the
	// resources/workshop.yaml file under the directory, the same as if a
	// directory path was provided explicitly.

	if path == "" {
		path = "."
	}

	// Load the workshop definition. The path can be a HTTP/HTTPS URL for a
	// local file system path for a directory or file.

	var workshop *unstructured.Unstructured

	if workshop, err = loadWorkshopDefinition(o.Name, path, o.Portal, o.WorkshopFile, o.WorkshopVersion, o.DataValuesFlags); err != nil {
		return err
	}

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	dynamicClient, err := clusterConfig.GetDynamicClient()

	if err != nil {
		return errors.Wrapf(err, "unable to create Kubernetes client")
	}

	// Update the workshop resource in the Kubernetes cluster.

	err = updateWorkshopResource(dynamicClient, workshop)

	if err != nil {
		return err
	}

	fmt.Printf("Loaded workshop %q.\n", workshop.GetName())

	// Update the training portal, creating it if necessary.

	err = deployWorkshopResource(dynamicClient, workshop, o.Alias, o.Portal, o.Capacity, o.Reserved, o.Initial, o.Expires, o.Overtime, o.Deadline, o.Orphaned, o.Overdue, o.Refresh, o.Repository, o.Environ, o.Labels, o.OpenBrowser)

	if err != nil {
		return err
	}

	return nil
}

func (p *ProjectInfo) NewClusterWorkshopDeployCmd() *cobra.Command {
	var o ClusterWorkshopDeployOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "deploy",
		Short: "Deploy workshop to Kubernetes",
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
		&o.Alias,
		"alias",
		"a",
		"",
		"alias to be used to identify the workshop by the training portal",
	)
	c.Flags().StringVarP(
		&o.Path,
		"file",
		"f",
		".",
		"path to local workshop directory, definition file, or URL for workshop definition file",
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
		"name to be used for training portal and workshop name prefixes",
	)
	c.Flags().UintVar(
		&o.Capacity,
		"capacity",
		0,
		"maximum number of concurrent sessions for this workshop",
	)
	c.Flags().UintVar(
		&o.Reserved,
		"reserved",
		0,
		"number of workshop sessions to maintain ready in reserve",
	)
	c.Flags().UintVar(
		&o.Initial,
		"initial",
		0,
		"number of workshop sessions to create when first deployed",
	)
	c.Flags().StringVar(
		&o.Expires,
		"expires",
		"",
		"time duration before the workshop is expired",
	)
	c.Flags().StringVar(
		&o.Overtime,
		"overtime",
		"",
		"time extension allowed for the workshop",
	)
	c.Flags().StringVar(
		&o.Deadline,
		"deadline",
		"",
		"maximum time duration allowed for the workshop",
	)
	c.Flags().StringVar(
		&o.Orphaned,
		"orphaned",
		"5m",
		"allowed inactive time before workshop is terminated",
	)
	c.Flags().StringVar(
		&o.Overdue,
		"overdue",
		"2m",
		"allowed startup time before workshop is deemed failed",
	)
	c.Flags().StringVar(
		&o.Refresh,
		"refresh",
		"",
		"interval after which workshop environment is recreated",
	)
	c.Flags().StringSliceVarP(
		&o.Environ,
		"env",
		"e",
		[]string{},
		"environment variable overrides for workshop",
	)
	c.Flags().StringSliceVarP(
		&o.Labels,
		"labels",
		"l",
		[]string{},
		"label overrides for workshop",
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

	c.Flags().StringVar(
		&o.Repository,
		"image-repository",
		"",
		"the address of the image repository",
	)

	c.Flags().BoolVar(
		&o.OpenBrowser,
		"open-browser",
		false,
		"automatically launch browser on portal",
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

var trainingPortalResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "trainingportals"}

func deployWorkshopResource(client dynamic.Interface, workshop *unstructured.Unstructured, alias string, portal string, capacity uint, reserved uint, initial uint, expires string, overtime string, deadline string, orphaned string, overdue string, refresh string, registry string, environ []string, labels []string, openBrowser bool) error {
	trainingPortalClient := client.Resource(trainingPortalResource)

	trainingPortal, err := trainingPortalClient.Get(context.TODO(), portal, metav1.GetOptions{})

	var trainingPortalExists = true

	if k8serrors.IsNotFound(err) {
		trainingPortalExists = false

		trainingPortal = &unstructured.Unstructured{}

		trainingPortal.SetUnstructuredContent(map[string]interface{}{
			"apiVersion": "training.educates.dev/v1beta1",
			"kind":       "TrainingPortal",
			"metadata": map[string]interface{}{
				"name": portal,
			},
			"spec": map[string]interface{}{
				"portal": map[string]interface{}{
					"password": randomPassword(12),
					"registration": struct {
						Type string `json:"type"`
					}{
						Type: "anonymous",
					},
					"updates": struct {
						Workshop bool `json:"workshop"`
					}{
						Workshop: true,
					},
					"sessions": struct {
						Maximum int64 `json:"maximum"`
					}{
						Maximum: 5,
					},
					"workshop": map[string]interface{}{
						"defaults": struct {
							Reserved int `json:"reserved"`
						}{
							Reserved: 0,
						},
					},
				},
				"workshops": []interface{}{},
			},
		})
	}

	workshops, _, err := unstructured.NestedSlice(trainingPortal.Object, "spec", "workshops")

	if err != nil {
		return errors.Wrap(err, "unable to retrieve workshops from training portal")
	}

	var updatedWorkshops []interface{}

	if expires == "" {
		duration, propertyExists, err := unstructured.NestedString(workshop.Object, "spec", "duration")

		if err != nil || !propertyExists {
			expires = "60m"
		} else {
			expires = duration
		}
	}

	type EnvironDetails struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}

	var environVariables []EnvironDetails

	for _, value := range environ {
		parts := strings.SplitN(value, "=", 2)
		environVariables = append(environVariables, EnvironDetails{
			Name:  parts[0],
			Value: parts[1],
		})
	}

	type LabelDetails struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}

	var labelOverrides []LabelDetails

	for _, value := range labels {
		parts := strings.SplitN(value, "=", 2)
		labelOverrides = append(labelOverrides, LabelDetails{
			Name:  parts[0],
			Value: parts[1],
		})
	}

	var foundWorkshop = false

	for _, item := range workshops {
		object := item.(map[string]interface{})

		updatedWorkshops = append(updatedWorkshops, object)

		if object["name"] == workshop.GetName() && object["alias"] == alias {
			foundWorkshop = true

			object["reserved"] = int64(reserved)
			object["initial"] = int64(initial)

			if capacity != 0 {
				object["capacity"] = int64(capacity)
			} else {
				delete(object, "capacity")
			}

			if expires != "" {
				object["expires"] = expires
			} else {
				delete(object, "expires")
			}

			if overtime != "" {
				object["overtime"] = overtime
			} else {
				delete(object, "overtime")
			}

			if deadline != "" {
				object["deadline"] = deadline
			} else {
				delete(object, "deadline")
			}

			if orphaned != "" {
				object["orphaned"] = orphaned
			} else {
				delete(object, "orphaned")
			}

			if overdue != "" {
				object["overdue"] = overdue
			} else {
				delete(object, "overdue")
			}

			if refresh != "" {
				object["refresh"] = refresh
			} else {
				delete(object, "refresh")
			}

			var tmpEnvironVariables []interface{}

			for _, item := range environVariables {
				tmpEnvironVariables = append(tmpEnvironVariables, map[string]interface{}{
					"name":  item.Name,
					"value": item.Value,
				})
			}

			object["env"] = tmpEnvironVariables

			var tmpLabelOverrides []interface{}

			for _, item := range labelOverrides {
				tmpLabelOverrides = append(tmpLabelOverrides, map[string]interface{}{
					"name":  item.Name,
					"value": item.Value,
				})
			}

			object["labels"] = tmpLabelOverrides
		}
	}

	type RegistryDetails struct {
		Host      string `json:"host"`
		Namespace string `json:"namespace,omitempty"`
	}

	type WorkshopDetails struct {
		Name     string           `json:"name"`
		Alias    string           `json:"alias"`
		Capacity int64            `json:"capacity,omitempty"`
		Initial  int64            `json:"initial"`
		Reserved int64            `json:"reserved"`
		Expires  string           `json:"expires,omitempty"`
		Overtime string           `json:"overtime,omitempty"`
		Deadline string           `json:"deadline,omitempty"`
		Orphaned string           `json:"orphaned,omitempty"`
		Overdue  string           `json:"overdue,omitempty"`
		Refresh  string           `json:"refresh,omitempty"`
		Registry *RegistryDetails `json:"registry,omitempty"`
		Environ  []EnvironDetails `json:"env"`
		Labels   []LabelDetails   `json:"labels"`
	}

	if !foundWorkshop {
		workshopDetails := WorkshopDetails{
			Name:     workshop.GetName(),
			Alias:    alias,
			Initial:  int64(initial),
			Reserved: int64(reserved),
			Expires:  expires,
			Overtime: overtime,
			Deadline: deadline,
			Orphaned: orphaned,
			Overdue:  overdue,
			Refresh:  refresh,
			Environ:  environVariables,
			Labels:   labelOverrides,
		}

		if capacity != 0 {
			workshopDetails.Capacity = int64(capacity)
		}

		if registry != "" {
			parts := strings.SplitN(registry, "/", 2)

			host := parts[0]
			var namespace string

			if len(parts) > 1 {
				namespace = parts[1]
			}

			registryDetails := RegistryDetails{
				Host:      host,
				Namespace: namespace,
			}

			workshopDetails.Registry = &registryDetails
		}

		var workshopDetailsMap map[string]interface{}

		data, _ := json.Marshal(workshopDetails)
		json.Unmarshal(data, &workshopDetailsMap)

		updatedWorkshops = append(updatedWorkshops, workshopDetailsMap)
	}

	unstructured.SetNestedSlice(trainingPortal.Object, updatedWorkshops, "spec", "workshops")

	if trainingPortalExists {
		fmt.Printf("Updating existing training portal %q.\n", trainingPortal.GetName())
		_, err = trainingPortalClient.Update(context.TODO(), trainingPortal, metav1.UpdateOptions{FieldManager: constants.DefaultPortalName})
	} else {
		fmt.Printf("Creating new training portal %q.\n", trainingPortal.GetName())
		_, err = trainingPortalClient.Create(context.TODO(), trainingPortal, metav1.CreateOptions{FieldManager: constants.DefaultPortalName})
	}

	if err != nil {
		return errors.Wrapf(err, "unable to update training portal %q in cluster", portal)
	}

	fmt.Print("Workshop added to training portal.\n")

	if openBrowser {
		// Need to refetch training portal because if was just created the URL
		// for access may not have been set yet.

		var targetUrl string

		fmt.Print("Checking training portal is ready.\n")

		spinner := func(iteration int) string {
			spinners := `|/-\`
			return string(spinners[iteration%len(spinners)])
		}

		for i := 1; i < 60; i++ {
			fmt.Printf("\r[%s] Waiting...", spinner(i))

			time.Sleep(time.Second)

			trainingPortal, err = trainingPortalClient.Get(context.TODO(), portal, metav1.GetOptions{})

			if err != nil {
				return errors.Wrapf(err, "unable to fetch training portal %q in cluster", portal)
			}

			var found bool

			targetUrl, found, _ = unstructured.NestedString(trainingPortal.Object, "status", "educates", "url")

			if found {
				break
			}
		}

		rootUrl := targetUrl

		password, _, _ := unstructured.NestedString(trainingPortal.Object, "spec", "portal", "password")

		if password != "" {
			values := url.Values{}
			values.Add("redirect_url", "/")
			values.Add("password", password)

			targetUrl = fmt.Sprintf("%s/workshops/access/?%s", targetUrl, values.Encode())
		}

		for i := 1; i < 300; i++ {
			fmt.Printf("\r[%s] Waiting...", spinner(i))

			time.Sleep(time.Second)

			resp, err := http.Get(rootUrl)

			if err != nil || resp.StatusCode == 503 {
				continue
			}

			defer resp.Body.Close()
			io.ReadAll(resp.Body)

			break
		}

		fmt.Print("\r              \r")

		fmt.Printf("Opening training portal %s.\n", targetUrl)

		switch runtime.GOOS {
		case "linux":
			err = exec.Command("xdg-open", targetUrl).Start()
		case "windows":
			err = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetUrl).Start()
		case "darwin":
			err = exec.Command("open", targetUrl).Start()
		default:
			err = fmt.Errorf("unsupported platform")
		}

		if err != nil {
			return errors.Wrap(err, "unable to open web browser")
		}
	}

	return nil
}

func randomPassword(length int) string {
	rand.New(rand.NewSource(int64(new(maphash.Hash).Sum64())))

	chars := []rune("!#%+23456789:=?@ABCDEFGHJKLMNPRSTUVWXYZabcdefghijkmnopqrstuvwxyz")

	var b strings.Builder

	for i := 0; i < length; i++ {
		b.WriteRune(chars[rand.Intn(len(chars))])
	}
	return b.String()
}
