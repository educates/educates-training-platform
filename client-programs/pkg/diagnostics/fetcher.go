package diagnostics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	educatesrestapi "github.com/educates/educates-training-platform/client-programs/pkg/educates/restapi"
	educatesTypes "github.com/educates/educates-training-platform/client-programs/pkg/educates/types"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/printers"
)


type ClusterDiagnosticsFetcher struct {
	clusterConfig *cluster.ClusterConfig
	tempDir       string
	verbose       bool
}

func (c *ClusterDiagnosticsFetcher) getEducatesNamespaces(fileName string) error {
	client, err := c.clusterConfig.GetClient()
	if err != nil {
		return err
	}

	namespaces, err := client.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{
		// LabelSelector: constants.EducatesWorkshopLabelAnnotationComponent,
	})
	if err != nil {
		return err
	}

	newFile, err := os.Create(filepath.Join(c.tempDir, fileName))
	if err != nil {
		return err
	}
	defer newFile.Close()

	y := printers.YAMLPrinter{}
	for _, object := range namespaces.Items {
		object.SetManagedFields(nil) // Remove managedFields from the object
		// We need to add the GroupVersionKind to the object, as it is not set by default. See: https://github.com/kubernetes-sigs/controller-runtime/issues/1517
		object.GetObjectKind().SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"})
		if err := y.PrintObj(&object, newFile); err != nil {
			return err
		}
	}

	if c.verbose {
		fmt.Printf("Educates namespaces saved in file: %v\n", fileName)
	}

	return nil
}

// TODO: Print events in a more human readable format
func (c *ClusterDiagnosticsFetcher) getEducatesNamespacesEvents(fileName string) error {
	client, err := c.clusterConfig.GetClient()
	if err != nil {
		return err
	}

	namespaces, err := client.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	newFile, err := os.Create(filepath.Join(c.tempDir, fileName))
	if err != nil {
		return err
	}
	defer newFile.Close()

	y := printers.YAMLPrinter{}
	for _, namespace := range namespaces.Items {
		if !strings.HasPrefix(namespace.Labels["kubernetes.io/metadata.name"], constants.EducatesNamespaceLabelMetadataName) {
			continue
		}
		events, err := client.CoreV1().Events(namespace.Name).List(context.TODO(), metav1.ListOptions{
			// LabelSelector: constants.EducatesWorkshopLabelAnnotationComponent,
		})
		for _, object := range events.Items {
			object.SetManagedFields(nil) // Remove managedFields from the object
			// We need to add the GroupVersionKind to the object, as it is not set by default. See: https://github.com/kubernetes-sigs/controller-runtime/issues/1517
			object.GetObjectKind().SetGroupVersionKind(schema.GroupVersionKind{Group: "events.k8s.io", Version: "v1", Kind: "Event"})
			if err := y.PrintObj(&object, newFile); err != nil {
				return err
			}
		}
		if err != nil {
			return err
		}
	}
	if c.verbose {
		fmt.Printf("Educates namespaces events saved in file: %v\n", fileName)
	}

	return nil
}

func (c *ClusterDiagnosticsFetcher) fetchDynamicallyResources(res schema.GroupVersionResource, fileName string) error {
	dynamicClient, err := c.clusterConfig.GetDynamicClient()
	if err != nil {
		return err
	}
	dynClient := dynamicClient.Resource(res)

	objectList, err := dynClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	newFile, err := os.Create(filepath.Join(c.tempDir, fileName))
	if err != nil {
		return err
	}
	defer newFile.Close()

	y := printers.YAMLPrinter{}
	for _, object := range objectList.Items {
		object.SetManagedFields(nil) // Remove managedFields from the object
		if err := y.PrintObj(&object, newFile); err != nil {
			return err
		}
	}

	if c.verbose {
		fmt.Printf("Educates %v saved in file: %v\n", res.Resource, fileName)
	}

	return nil
}

func (c *ClusterDiagnosticsFetcher) fetchLogsForDeployment(labelSelector, namespaceSelector, fileNamePattern string) error {
	client, err := c.clusterConfig.GetClient()
	if err != nil {
		return err
	}
	// Create an array of strings to store the namespaces
	var namespacesList []string
	if strings.Contains(namespaceSelector, "=") {
		namespaces, err := client.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{
			LabelSelector: namespaceSelector,
		})
		if err != nil {
			return err
		}
		for _, namespace := range namespaces.Items {
			namespacesList = append(namespacesList, namespace.Name)
		}
	} else {
		namespacesList = strings.Split(namespaceSelector, ",")
	}

	for _, namespaceName := range namespacesList {
		pods, err := client.CoreV1().Pods(namespaceName).List(context.TODO(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return err
		}

		if strings.Contains(fileNamePattern, "%v") {
			fileNamePattern = fmt.Sprintf(fileNamePattern, namespaceName)
		}

		logFile, err := os.Create(filepath.Join(c.tempDir, fileNamePattern))
		if err != nil {
			return err
		}
		defer logFile.Close()

		for _, pod := range pods.Items {
			req := client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, &v1.PodLogOptions{Timestamps: true})
			podLogs, err := req.Stream(context.TODO())
			if err != nil {
				return err
			}
			defer podLogs.Close()

			_, err = io.Copy(logFile, podLogs)
			if err != nil {
				return err
			}

			if c.verbose {
				fmt.Printf("Educates related logs saved in file: %v\n", fileNamePattern)
			}
		}
	}

	return nil
}

func (c *ClusterDiagnosticsFetcher) fetchTrainingPortalDetailsAtRest(fileNamePattern string) error {
	dynamicClient, err := c.clusterConfig.GetDynamicClient()
	if err != nil {
		return err
	}
	dynClient := dynamicClient.Resource(educatesTypes.TrainingPortalResource)
	trainingPortals, err := dynClient.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}
	// Iterate over the training portals and get the details of the workshop sessions
	for _, trainingPortal := range trainingPortals.Items {
		trainingPortalName, _, _ := unstructured.NestedString(trainingPortal.Object, "metadata", "name")

		catalogApiRequester := educatesrestapi.NewWorkshopsCatalogRequester(
			c.clusterConfig,
			trainingPortalName,
		)
		logout, err := catalogApiRequester.Login()
		defer logout()
		if err != nil {
			return errors.Wrap(err, "failed to login to training portal")
		}

		// Get the list of workshops so we can know which workshop environment
		// we need to request a workshop from.
		listEnvironmentsResult, err := catalogApiRequester.GetWorkshopsCatalog()
		if err != nil {
			return errors.Wrap(err, "failed to get workshops catalog")
		}

		if strings.Contains(fileNamePattern, "%v") {
			fileNamePattern = fmt.Sprintf(fileNamePattern, trainingPortalName)
		}
		newFile, err := os.Create(filepath.Join(c.tempDir, fileNamePattern))
		if err != nil {
			return err
		}
		defer newFile.Close()

		// Pretty print in json format listEnvironmentsResult
		prettyListEnvironmentsResult, err := json.MarshalIndent(listEnvironmentsResult, "", "  ")
		if err != nil {
			return err
		}
		// print into newFile
		_, err = newFile.Write(prettyListEnvironmentsResult)
		if err != nil {
			return err
		}

		if c.verbose {
			fmt.Printf("Educates trainingportal details at rest saved in file: %v\n", fileNamePattern)
		}

	}
	return nil
}
