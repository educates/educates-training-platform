package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	educatesTypes "github.com/educates/educates-training-platform/client-programs/pkg/educates/types"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"

	// "github.com/educates/educates-training-platform/client-programs/pkg/workshops"
	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

type WorkshopManager struct {
	Client dynamic.Interface
}

func NewWorkshopManager(client dynamic.Interface) *WorkshopManager {
	return &WorkshopManager{Client: client}
}

type DeployWorkshopConfig struct {
	Workshop *unstructured.Unstructured
	Alias string
	Portal string
	Capacity uint
	Reserved uint
	Initial uint
	Expires string
	Overtime string
	Deadline string
	Orphaned string
	Overdue string
	Refresh string
	Registry string
	Environ []string
	Labels []string
}

type UpdateWorkshopResourceConfig struct {
	Workshop *unstructured.Unstructured
}

type ListWorkshopResourcesConfig struct {
	Portal string
}

type DeleteWorkshopResourceConfig struct {
	Name string
	Alias string
	Portal string
}

type OpenBrowserConfig struct {
	Portal string
}

func (m *WorkshopManager) DeployWorkshopResource(o *DeployWorkshopConfig) error {
	trainingPortalClient := m.Client.Resource(educatesTypes.TrainingPortalResource)

	trainingPortal, err := trainingPortalClient.Get(context.TODO(), o.Portal, metav1.GetOptions{})

	var trainingPortalExists = true

	if k8serrors.IsNotFound(err) {
		trainingPortalExists = false

		trainingPortal = &unstructured.Unstructured{}

		trainingPortal.SetUnstructuredContent(map[string]interface{}{
			"apiVersion": constants.EducatesTrainingAPIGroupVersion,
			"kind":       "TrainingPortal",
			"metadata": map[string]interface{}{
				"name": o.Portal,
			},
			"spec": map[string]interface{}{
				"portal": map[string]interface{}{
					"password": utils.RandomPassword(12),
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

	if o.Expires == "" {
		duration, propertyExists, err := unstructured.NestedString(o.Workshop.Object, "spec", "duration")

		if err != nil || !propertyExists {
			o.Expires = "60m"
		} else {
			o.Expires = duration
		}
	}

	type EnvironDetails struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}

	var environVariables []EnvironDetails

	for _, value := range o.Environ {
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

	for _, value := range o.Labels {
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

		if object["name"] == o.Workshop.GetName() && object["alias"] == o.Alias {
			foundWorkshop = true

			object["reserved"] = int64(o.Reserved)
			object["initial"] = int64(o.Initial)

			if o.Capacity != 0 {
				object["capacity"] = int64(o.Capacity)
			} else {
				delete(object, "capacity")
			}

			if o.Expires != "" {
				object["expires"] = o.Expires
			} else {
				delete(object, "expires")
			}

			if o.Overtime != "" {
				object["overtime"] = o.Overtime
			} else {
				delete(object, "overtime")
			}

			if o.Deadline != "" {
				object["deadline"] = o.Deadline
			} else {
				delete(object, "deadline")
			}

			if o.Orphaned != "" {
				object["orphaned"] = o.Orphaned
			} else {
				delete(object, "orphaned")
			}

			if o.Overdue != "" {
				object["overdue"] = o.Overdue
			} else {
				delete(object, "overdue")
			}

			if o.Refresh != "" {
				object["refresh"] = o.Refresh
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
			Name:     o.Workshop.GetName(),
			Alias:    o.Alias,
			Initial:  int64(o.Initial),
			Reserved: int64(o.Reserved),
			Expires:  o.Expires,
			Overtime: o.Overtime,
			Deadline: o.Deadline,
			Orphaned: o.Orphaned,
			Overdue:  o.Overdue,
			Refresh:  o.Refresh,
			Environ:  environVariables,
			Labels:   labelOverrides,
		}

		if o.Capacity != 0 {
			workshopDetails.Capacity = int64(o.Capacity)
		}

		if o.Registry != "" {
			parts := strings.SplitN(o.Registry, "/", 2)

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
		return errors.Wrapf(err, "unable to update training portal %q in cluster", o.Portal)
	}

	fmt.Print("Workshop added to training portal.\n")

	return nil
}


func (m *WorkshopManager) UpdateWorkshopResource(o *UpdateWorkshopResourceConfig) error {
	workshopsClient := m.Client.Resource(educatesTypes.WorkshopResource)

	// _, err := workshopsClient.Apply(context.TODO(), workshop.GetName(), workshop, metav1.ApplyOptions{FieldManager: constants.DefaultPortalName, Force: true})

	workshopBytes, err := runtime.Encode(unstructured.UnstructuredJSONScheme, o.Workshop)

	if err != nil {
		return errors.Wrapf(err, "unable to update workshop definition in cluster %q", o.Workshop.GetName())
	}

	_, err = workshopsClient.Patch(context.TODO(), o.Workshop.GetName(), types.ApplyPatchType, workshopBytes, metav1.ApplyOptions{FieldManager: constants.DefaultPortalName, Force: true}.ToPatchOptions())

	if err != nil {
		return errors.Wrapf(err, "unable to update workshop definition in cluster %q", o.Workshop.GetName())
	}

	return nil
}

func (m *WorkshopManager) ListWorkshopResources(o *ListWorkshopResourcesConfig) (string, error) {
	trainingPortalClient := m.Client.Resource(educatesTypes.TrainingPortalResource)

	trainingPortal, err := trainingPortalClient.Get(context.TODO(), o.Portal, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		return "No workshops found.", nil
	}

	sessionsMaximum, sessionsMaximumExists, _ := unstructured.NestedInt64(trainingPortal.Object, "spec", "portal", "sessions", "maximum")

	workshops, _, err := unstructured.NestedSlice(trainingPortal.Object, "spec", "workshops")

	if err != nil {
		return "", errors.Wrap(err, "unable to retrieve workshops from training portal")
	}

	if len(workshops) == 0 {
		return "No workshops found.", nil
	}

	workshopsClient := m.Client.Resource(educatesTypes.WorkshopResource)

	var data [][]string
	for _, item := range workshops {
		object := item.(map[string]interface{})
		name := object["name"].(string)
		alias := object["alias"].(string)

		var capacityField string

		capacity, capacityExists := object["capacity"]

		if capacityExists {
			capacityField = fmt.Sprintf("%d", capacity)
		} else if sessionsMaximumExists {
			capacityField = fmt.Sprintf("%d", sessionsMaximum)
		}

		workshop, err := workshopsClient.Get(context.TODO(), name, metav1.GetOptions{})

		source := ""

		if err == nil {
			annotations := workshop.GetAnnotations()

			if val, ok := annotations[constants.EducatesWorkshopLabelAnnotationSource]; ok {
				source = val
			}
		}

		data = append(data, []string{name, alias, capacityField, source})
	}

	return utils.PrintTable([]string{"NAME", "ALIAS", "CAPACITY", "SOURCE"}, data), nil
}

func (m *WorkshopManager) DeleteWorkshopResource(o *DeleteWorkshopResourceConfig) error {
	trainingPortalClient := m.Client.Resource(educatesTypes.TrainingPortalResource)

	trainingPortal, err := trainingPortalClient.Get(context.TODO(), o.Portal, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		return nil
	}

	workshops, _, err := unstructured.NestedSlice(trainingPortal.Object, "spec", "workshops")

	if err != nil {
		return errors.Wrap(err, "unable to retrieve workshops from training portal")
	}

	var found = false

	var updatedWorkshops []interface{}

	for _, item := range workshops {
		object := item.(map[string]interface{})

		if object["name"] != o.Name || object["alias"] != o.Alias {
			updatedWorkshops = append(updatedWorkshops, object)
		} else {
			found = true
		}
	}

	if !found {
		return nil
	}

	unstructured.SetNestedSlice(trainingPortal.Object, updatedWorkshops, "spec", "workshops")

	_, err = trainingPortalClient.Update(context.TODO(), trainingPortal, metav1.UpdateOptions{FieldManager: constants.DefaultPortalName})

	if err != nil {
		return errors.Wrapf(err, "unable to update training portal %q in cluster", o.Portal)
	}

	return nil
}

func (m *WorkshopManager) OpenBrowser(o *OpenBrowserConfig) error {
	trainingPortalClient := m.Client.Resource(educatesTypes.TrainingPortalResource)

	// Need to refetch training portal because if was just created the URL
	// for access may not have been set yet.

	var targetUrl string

	fmt.Print("Checking training portal is ready.\n")

	spinner := func(iteration int) string {
		spinners := `|/-\`
		return string(spinners[iteration%len(spinners)])
	}

	var trainingPortal *unstructured.Unstructured
	var found bool
	var err error

	for i := 1; i < 60; i++ {
		fmt.Printf("\r[%s] Waiting...", spinner(i))

		time.Sleep(time.Second)

		trainingPortal, err = trainingPortalClient.Get(context.TODO(), o.Portal, metav1.GetOptions{})

		if err != nil {
			return errors.Wrapf(err, "unable to fetch training portal %q in cluster", o.Portal)
		}

		targetUrl, found, _ = unstructured.NestedString(trainingPortal.Object, "status", "educates", "url")

		if found {
			break
		}
	}
	if !found {
		return errors.New("training portal not found")
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

	return utils.OpenBrowser(targetUrl)
}


