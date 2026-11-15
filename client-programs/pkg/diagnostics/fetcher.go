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
	"github.com/educates/educates-training-platform/client-programs/pkg/educatesrestapi"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/printers"
)

var workshopResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshops"}
var trainingportalResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "trainingportals"}
var workshopsessionsResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshopsessions"}
var workshoprequestsResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshoprequests"}
var workshopenvironmentsResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshopenvironments"}
var workshopallocationsResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshopallocations"}
var secretcopierResource = schema.GroupVersionResource{Group: "secrets.educates.dev", Version: "v1beta1", Resource: "secretcopiers"}
var secretinjectorsResource = schema.GroupVersionResource{Group: "secrets.educates.dev", Version: "v1beta1", Resource: "secretinjectors"}
var secretexportersResource = schema.GroupVersionResource{Group: "secrets.educates.dev", Version: "v1beta1", Resource: "secretexporters"}
var secretimportersResource = schema.GroupVersionResource{Group: "secrets.educates.dev", Version: "v1beta1", Resource: "secretimporters"}

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
		// LabelSelector: "training.educates.dev/component",
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
		if !strings.HasPrefix(namespace.Labels["kubernetes.io/metadata.name"], "educates") {
			continue
		}
		events, err := client.CoreV1().Events(namespace.Name).List(context.TODO(), metav1.ListOptions{
			// LabelSelector: "training.educates.dev/component",
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

	if err := writeResources(newFile, res, objectList.Items); err != nil {
		return err
	}

	if c.verbose {
		fmt.Printf("Educates %v saved in file: %v\n", res.Resource, fileName)
	}

	return nil
}

// redactedValue replaces a credential in the resources the bundle collects.
const redactedValue = "REDACTED"

// specCopyAnnotations are the annotations in which kopf records the spec of
// a resource it last handled, and kubectl apply the manifest it last
// applied, as JSON.
var specCopyAnnotations = []string{
	"kopf.zalando.org/last-handled-configuration",
	"kubectl.kubernetes.io/last-applied-configuration",
}

// writeResources writes the resources of kind res to w as YAML, without
// their managed fields. A TrainingPortal's inline credentials and OAuth
// clients, under spec.portal.credentials and spec.portal.clients, are written
// with every value redacted, in its spec and in the copies of its spec its
// annotations hold.
func writeResources(w io.Writer, res schema.GroupVersionResource, objects []unstructured.Unstructured) error {
	y := printers.YAMLPrinter{}
	for _, object := range objects {
		object.SetManagedFields(nil) // Remove managedFields from the object
		if res == trainingportalResource {
			redactInlineCredentials(object.Object)
			redactInlineCredentialsInSpecCopies(&object)
		}
		if err := y.PrintObj(&object, w); err != nil {
			return err
		}
	}

	return nil
}

// redactInlineCredentials redacts the inline credentials and OAuth clients
// of a TrainingPortal, or of a copy of its spec.
func redactInlineCredentials(obj map[string]interface{}) {
	redactNestedValues(obj, "spec", "portal", "credentials")
	redactNestedValues(obj, "spec", "portal", "clients")
}

// redactInlineCredentialsInSpecCopies redacts the inline credentials in the
// copies of a TrainingPortal's spec that its annotations hold. An annotation
// that cannot be read as JSON is replaced with redactedValue.
func redactInlineCredentialsInSpecCopies(object *unstructured.Unstructured) {
	annotations := object.GetAnnotations()
	changed := false
	for _, name := range specCopyAnnotations {
		value, found := annotations[name]
		if !found {
			continue
		}
		changed = true
		var copied map[string]interface{}
		if err := json.Unmarshal([]byte(value), &copied); err != nil {
			annotations[name] = redactedValue
			continue
		}
		redactInlineCredentials(copied)
		redacted, err := json.Marshal(copied)
		if err != nil {
			annotations[name] = redactedValue
			continue
		}
		annotations[name] = string(redacted)
	}
	if changed {
		object.SetAnnotations(annotations)
	}
}

// redactNestedValues replaces every value under the field at path in obj
// with redactedValue, keeping the keys, so the bundle shows which values were
// set without holding them.
func redactNestedValues(obj map[string]interface{}, path ...string) {
	field, found, err := unstructured.NestedFieldNoCopy(obj, path...)
	if err != nil || !found {
		return
	}
	_ = unstructured.SetNestedField(obj, redactValue(field), path...)
}

func redactValue(value interface{}) interface{} {
	nested, isMap := value.(map[string]interface{})
	if !isMap {
		return redactedValue
	}
	redacted := make(map[string]interface{}, len(nested))
	for key, value := range nested {
		redacted[key] = redactValue(value)
	}
	return redacted
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

		fileName := fileNamePattern
		if strings.Contains(fileNamePattern, "%v") {
			fileName = fmt.Sprintf(fileNamePattern, namespaceName)
		}

		logFile, err := os.Create(filepath.Join(c.tempDir, fileName))
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
				fmt.Printf("Educates related logs saved in file: %v\n", fileName)
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
	dynClient := dynamicClient.Resource(trainingportalResource)
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
		if err != nil {
			return errors.Wrap(err, "failed to login to training portal")
		}
		defer logout()

		// Get the list of workshops so we can know which workshop environment
		// we need to request a workshop from.
		listEnvironmentsResult, err := catalogApiRequester.GetWorkshopsCatalog()
		if err != nil {
			return errors.Wrap(err, "failed to get workshops catalog")
		}

		fileName := fileNamePattern
		if strings.Contains(fileNamePattern, "%v") {
			fileName = fmt.Sprintf(fileNamePattern, trainingPortalName)
		}
		newFile, err := os.Create(filepath.Join(c.tempDir, fileName))
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
			fmt.Printf("Educates trainingportal details at rest saved in file: %v\n", fileName)
		}

	}
	return nil
}
