package cmd

import (
	"errors"
	"testing"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

const sessionManagerFinalizer = "kopf.zalando.org/KopfFinalizerMarker"

func testTrainingPortal(name string, workshops ...string) *unstructured.Unstructured {
	entries := []interface{}{}
	for _, workshop := range workshops {
		entries = append(entries, map[string]interface{}{"name": workshop, "alias": ""})
	}

	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "training.educates.dev/v1beta1",
		"kind":       "TrainingPortal",
		"metadata":   map[string]interface{}{"name": name},
		"spec":       map[string]interface{}{"workshops": entries},
	}}
}

func testWorkshop(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "training.educates.dev/v1beta1",
		"kind":       "Workshop",
		"metadata":   map[string]interface{}{"name": name},
		"spec":       map[string]interface{}{"duration": "30m"},
	}}
}

func newTrainingPortalClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{trainingPortalResource: "TrainingPortalList"},
		objects...,
	)
}

func deployTestWorkshop(client *dynamicfake.FakeDynamicClient, workshop string, portal string) error {
	return deployWorkshopResource(client, testWorkshop(workshop), "", portal, 0, 0, 0, "", "", "", "", "", "", "", nil, nil, false)
}

func storedTrainingPortal(t *testing.T, client *dynamicfake.FakeDynamicClient, name string) *unstructured.Unstructured {
	t.Helper()

	object, err := client.Tracker().Get(trainingPortalResource, "", name)
	if err != nil {
		t.Fatalf("get training portal %q: %v", name, err)
	}

	return object.(*unstructured.Unstructured)
}

func portalWorkshopNames(t *testing.T, portal *unstructured.Unstructured) []string {
	t.Helper()

	workshops, _, err := unstructured.NestedSlice(portal.Object, "spec", "workshops")
	if err != nil {
		t.Fatalf("read spec.workshops: %v", err)
	}

	var names []string
	for _, item := range workshops {
		names = append(names, item.(map[string]interface{})["name"].(string))
	}

	return names
}

// writeConcurrently stands in for another writer of the portal, such as
// session-manager adding its finalizer, between the deploy's read of the
// portal and its write.
func writeConcurrently(t *testing.T, client *dynamicfake.FakeDynamicClient, portal *unstructured.Unstructured) {
	t.Helper()

	portal = portal.DeepCopy()
	portal.SetFinalizers([]string{sessionManagerFinalizer})

	var err error
	if _, getErr := client.Tracker().Get(trainingPortalResource, "", portal.GetName()); getErr == nil {
		err = client.Tracker().Update(trainingPortalResource, portal, "")
	} else {
		err = client.Tracker().Create(trainingPortalResource, portal, "")
	}
	if err != nil {
		t.Fatalf("concurrent write of training portal: %v", err)
	}
}

func TestDeployWorkshopResource_RetriesUpdateOnConflict(t *testing.T) {
	client := newTrainingPortalClient(testTrainingPortal("educates-cli", "lab-one"))

	updates := 0
	client.PrependReactor("update", "trainingportals", func(action clienttesting.Action) (bool, runtime.Object, error) {
		updates++
		if updates > 1 {
			return false, nil, nil
		}

		writeConcurrently(t, client, storedTrainingPortal(t, client, "educates-cli"))

		return true, nil, k8serrors.NewConflict(
			trainingPortalResource.GroupResource(), "educates-cli",
			errors.New("the object has been modified; please apply your changes to the latest version and try again"),
		)
	})

	if err := deployTestWorkshop(client, "lab-two", "educates-cli"); err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	if updates != 2 {
		t.Errorf("got %d updates, want 2: one conflict and one retry", updates)
	}

	portal := storedTrainingPortal(t, client, "educates-cli")

	if got := portalWorkshopNames(t, portal); len(got) != 2 || got[0] != "lab-one" || got[1] != "lab-two" {
		t.Errorf("got workshops %v, want [lab-one lab-two]", got)
	}

	if got := portal.GetFinalizers(); len(got) != 1 || got[0] != sessionManagerFinalizer {
		t.Errorf("got finalizers %v, want the concurrent write's %q kept", got, sessionManagerFinalizer)
	}
}

func TestDeployWorkshopResource_UpdatesPortalCreatedConcurrently(t *testing.T) {
	client := newTrainingPortalClient()

	creates := 0
	client.PrependReactor("create", "trainingportals", func(action clienttesting.Action) (bool, runtime.Object, error) {
		creates++

		writeConcurrently(t, client, testTrainingPortal("educates-cli", "lab-one"))

		return true, nil, k8serrors.NewAlreadyExists(trainingPortalResource.GroupResource(), "educates-cli")
	})

	if err := deployTestWorkshop(client, "lab-two", "educates-cli"); err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	if creates != 1 {
		t.Errorf("got %d creates, want 1", creates)
	}

	portal := storedTrainingPortal(t, client, "educates-cli")

	if got := portalWorkshopNames(t, portal); len(got) != 2 || got[0] != "lab-one" || got[1] != "lab-two" {
		t.Errorf("got workshops %v, want [lab-one lab-two]", got)
	}

	if got := portal.GetFinalizers(); len(got) != 1 || got[0] != sessionManagerFinalizer {
		t.Errorf("got finalizers %v, want the concurrent write's %q kept", got, sessionManagerFinalizer)
	}
}

func TestDeployWorkshopResource_CreatesMissingPortal(t *testing.T) {
	client := newTrainingPortalClient()

	if err := deployTestWorkshop(client, "lab-one", "educates-cli"); err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	portal := storedTrainingPortal(t, client, "educates-cli")

	if got := portalWorkshopNames(t, portal); len(got) != 1 || got[0] != "lab-one" {
		t.Errorf("got workshops %v, want [lab-one]", got)
	}

	registration, _, _ := unstructured.NestedString(portal.Object, "spec", "portal", "registration", "type")
	if registration != "anonymous" {
		t.Errorf("got registration type %q, want anonymous", registration)
	}
}
