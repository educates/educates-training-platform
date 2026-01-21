package portal

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	educatesTypes "github.com/educates/educates-training-platform/client-programs/pkg/educates/types"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

type PortalManager struct {
	client dynamic.Interface
}

type TrainingPortalCreateConfig struct {
	Portal       string
	Hostname     string
	Repository   string
	Capacity     uint
	Password     string
	IsPasswordSet	bool
	ThemeName    string
	CookieDomain string
	Labels       []string
}

type TrainingPortalDeleteConfig struct {
	Portal string
}

type TrainingPortalListConfig struct {
}

type TrainingPortalOpenConfig struct {
	Portal string
	Admin  bool
}

type TrainingPortalPasswordConfig struct {
	Portal string
	Admin  bool
}

func NewPortalManager(client dynamic.Interface) *PortalManager {
	return &PortalManager{client: client}
}

func (m *PortalManager) CreateTrainingPortal(cfg *TrainingPortalCreateConfig) error {
	trainingPortalClient := m.client.Resource(educatesTypes.TrainingPortalResource)

	_, err := trainingPortalClient.Get(context.TODO(), cfg.Portal, metav1.GetOptions{})

	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return errors.Wrap(err, "unable to query training portal")
		}
	} else {
		return errors.New("training portal already exists")
	}

	trainingPortal := &unstructured.Unstructured{}

	if !cfg.IsPasswordSet {
		cfg.Password = utils.RandomPassword(12)
	}

	type LabelDetails struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}

	var labelOverrides []LabelDetails

	for _, value := range cfg.Labels {
		parts := strings.SplitN(value, "=", 2)
		labelOverrides = append(labelOverrides, LabelDetails{
			Name:  parts[0],
			Value: parts[1],
		})
	}

	type RegistryDetails struct {
		Host      string `json:"host"`
		Namespace string `json:"namespace"`
	}

	registryHost := ""
	registryNamespace := ""

	if cfg.Repository != "" {
		parts := strings.SplitN(cfg.Repository, "/", 2)

		registryHost = parts[0]

		if len(parts) > 1 {
			registryNamespace = parts[1]
		}

	}

	trainingPortal.SetUnstructuredContent(map[string]interface{}{
		"apiVersion": "training.educates.dev/v1beta1",
		"kind":       "TrainingPortal",
		"metadata": map[string]interface{}{
			"name": cfg.Portal,
		},
		"spec": map[string]interface{}{
			"portal": map[string]interface{}{
				"password": cfg.Password,
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
					Maximum: int64(cfg.Capacity),
				},
				"workshop": map[string]interface{}{
					"defaults": struct {
						Reserved int             `json:"reserved"`
						Registry RegistryDetails `json:"registry"`
					}{
						Reserved: 0,
						Registry: RegistryDetails{
							Host:      registryHost,
							Namespace: registryNamespace,
						},
					},
				},
				"ingress": struct {
					Hostname string `json:"hostname"`
				}{
					Hostname: cfg.Hostname,
				},
				"theme": struct {
					Name string `json:"name"`
				}{
					Name: cfg.ThemeName,
				},
				"cookies": struct {
					Domain string `json:"domain"`
				}{
					Domain: cfg.CookieDomain,
				},
				"labels": labelOverrides,
			},
			"workshops": []interface{}{},
		},
	})

	_, err = trainingPortalClient.Create(context.TODO(), trainingPortal, metav1.CreateOptions{FieldManager: constants.DefaultPortalName})

	if err != nil {
		return errors.Wrapf(err, "unable to create training portal %q in cluster", cfg.Portal)
	}

	return nil
}

func (m *PortalManager) DeleteTrainingPortal(cfg *TrainingPortalDeleteConfig) error {
	trainingPortalClient := m.client.Resource(educatesTypes.TrainingPortalResource)

	_, err := trainingPortalClient.Get(context.TODO(), cfg.Portal, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		return errors.New("no portal found")
	}

	err = trainingPortalClient.Delete(context.TODO(), cfg.Portal, metav1.DeleteOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to delete portal")
	}

	return nil
}


func (m *PortalManager) ListTrainingPortals(cfg *TrainingPortalListConfig) (string, error) {
	trainingPortalClient := m.client.Resource(educatesTypes.TrainingPortalResource)

	trainingPortals, err := trainingPortalClient.List(context.TODO(), metav1.ListOptions{})

	if k8serrors.IsNotFound(err) {
		fmt.Println("No portals found.")
		return "", nil
	}

	var data [][]string
	for _, item := range trainingPortals.Items {
		name := item.GetName()

		sessionsMaximum, propertyExists, err := unstructured.NestedInt64(item.Object, "spec", "portal", "sessions", "maximum")

		var capacity string

		if err == nil && propertyExists {
			capacity = fmt.Sprintf("%d", sessionsMaximum)
		}

		url, _, _ := unstructured.NestedString(item.Object, "status", "educates", "url")

		data = append(data, []string{name, capacity, url})
	}
	return utils.PrintTable([]string{"NAME", "CAPACITY", "URL"}, data), nil
}

func (m *PortalManager) GetTrainingPortalBrowserUrl(cfg *TrainingPortalOpenConfig) (string, error) {
	trainingPortalClient := m.client.Resource(educatesTypes.TrainingPortalResource)

	trainingPortal, err := trainingPortalClient.Get(context.TODO(), cfg.Portal, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		return "", errors.New("no workshops deployed")
	}

	targetUrl, found, _ := unstructured.NestedString(trainingPortal.Object, "status", "educates", "url")

	if !found {
		return "", errors.New("workshops not available")
	}

	if cfg.Admin {
		targetUrl = targetUrl + "/admin"
	} else {
		password, _, _ := unstructured.NestedString(trainingPortal.Object, "spec", "portal", "password")

		if password != "" {
			values := url.Values{}
			values.Add("redirect_url", "/")
			values.Add("password", password)

			targetUrl = fmt.Sprintf("%s/workshops/access/?%s", targetUrl, values.Encode())
		}
	}

	return targetUrl, nil
}

func (m *PortalManager) GetTrainingPortalPassword(cfg *TrainingPortalPasswordConfig) (string, error) {
	trainingPortalClient := m.client.Resource(educatesTypes.TrainingPortalResource)

	trainingPortal, err := trainingPortalClient.Get(context.TODO(), cfg.Portal, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		return "", errors.New("no workshops deployed")
	}

	if cfg.Admin {
		username, found, err := unstructured.NestedString(trainingPortal.Object, "status", "educates", "credentials", "admin", "username")

		if err != nil || !found {
			return "", errors.New("unable to access credentials")
		}

		password, found, err := unstructured.NestedString(trainingPortal.Object, "status", "educates", "credentials", "admin", "password")

		if err != nil || !found {
			return "", errors.New("unable to access credentials")
		}

		return utils.PrintTable([]string{"USERNAME", "PASSWORD"}, [][]string{{username, password}}), nil
	} else {
		password, _, _ := unstructured.NestedString(trainingPortal.Object, "spec", "portal", "password")

		return password, nil
	}
}
