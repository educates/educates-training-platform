package rest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/educates/educates-training-platform/lookup-service/test/golden"
)

// The training resources the service watches on every cluster.
var (
	trainingGroupVersion    = schema.GroupVersion{Group: "training.educates.dev", Version: "v1beta1"}
	trainingPortalKind      = trainingGroupVersion.WithKind("TrainingPortal")
	workshopEnvironmentKind = trainingGroupVersion.WithKind("WorkshopEnvironment")
	workshopSessionKind     = trainingGroupVersion.WithKind("WorkshopSession")

	trainingKinds = []schema.GroupVersionKind{trainingPortalKind, workshopEnvironmentKind, workshopSessionKind}
)

// environmentNameLabel is the label naming a session's environment.
const environmentNameLabel = "training.educates.dev/environment.name"

// trainingState is the training resources of both clusters at one step of
// the golden scenario, as testdata/golden/states holds them.
type trainingState struct {
	Step        string                      `json:"step"`
	Description string                      `json:"description"`
	Clusters    map[string]clusterResources `json:"clusters"`
}

// clusterResources are the training resources of one cluster.
type clusterResources struct {
	TrainingPortals      []stateObject `json:"trainingPortals"`
	WorkshopEnvironments []stateObject `json:"workshopEnvironments"`
	WorkshopSessions     []stateObject `json:"workshopSessions"`
}

// stateObject is a training resource as a state records it, in placeholder
// form.
type stateObject struct {
	Name   string            `json:"name"`
	Labels map[string]string `json:"labels"`
	Spec   map[string]any    `json:"spec"`
	Status map[string]any    `json:"status"`
}

// loadState reads the training state of a step of the golden scenario.
func loadState(step string) *trainingState {
	GinkgoHelper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "states", step+".json"))
	Expect(err).NotTo(HaveOccurred())
	var state trainingState
	Expect(json.Unmarshal(data, &state)).To(Succeed())
	return &state
}

// applyState makes both clusters hold the training resources of a step of
// the golden scenario, creating, updating and deleting resources, and binds
// the placeholders of the objects it creates in session: their uids, the
// names of sessions and the users the portal generated.
func applyState(session *golden.Session, step string) {
	GinkgoHelper()
	state := loadState(step)
	for _, cluster := range []*testCluster{clusterA, clusterB} {
		resources := state.Clusters[cluster.name]
		cluster.apply(session, trainingPortalKind, resources.TrainingPortals)
		cluster.apply(session, workshopEnvironmentKind, resources.WorkshopEnvironments)
		cluster.apply(session, workshopSessionKind, resources.WorkshopSessions)
	}
}

// apply makes the cluster hold exactly objects of kind, parents being
// applied before their children.
func (c *testCluster) apply(session *golden.Session, kind schema.GroupVersionKind, objects []stateObject) {
	GinkgoHelper()
	wanted := map[string]bool{}
	for _, object := range objects {
		u := c.resolve(session, kind, object)
		wanted[u.GetName()] = true
		c.createOrUpdate(session, u)
	}

	existing := &unstructured.UnstructuredList{}
	existing.SetGroupVersionKind(kind.GroupVersion().WithKind(kind.Kind + "List"))
	Expect(c.client.List(context.Background(), existing)).To(Succeed())
	for i := range existing.Items {
		if !wanted[existing.Items[i].GetName()] {
			c.deleteObject(&existing.Items[i])
		}
	}
}

// resolve turns a state object into the resource to create, binding the
// placeholders that name it and expanding those it refers to.
func (c *testCluster) resolve(
	session *golden.Session, kind schema.GroupVersionKind, object stateObject,
) *unstructured.Unstructured {
	GinkgoHelper()
	name := object.Name
	if label, ok := strings.CutPrefix(name, "<session:"); ok {
		if bound, ok := session.Value(name); ok {
			name = bound
		} else {
			bound = strings.TrimSuffix(label, ">")
			session.Bind(name, bound)
			name = bound
		}
	}

	u := &unstructured.Unstructured{Object: map[string]any{}}
	u.SetGroupVersionKind(kind)
	u.SetName(name)
	if len(object.Labels) > 0 {
		labels := map[string]string{}
		for key, value := range object.Labels {
			if _, bound := session.Value(value); strings.HasPrefix(value, "<uid:") && !bound {
				// The object the label names has not been created yet, as
				// when a spec creates a session before its environment.
				continue
			}
			labels[key] = c.expand(session, value).(string)
		}
		u.SetLabels(labels)
	}
	spec := map[string]any{}
	if object.Spec != nil {
		spec = withoutNulls(c.expand(session, object.Spec)).(map[string]any)
	}
	if kind == workshopSessionKind {
		// The CRD requires the environment, which the service does not read.
		spec["environment"] = map[string]any{"name": object.Labels[environmentNameLabel]}
	}
	u.Object["spec"] = spec
	if object.Status != nil {
		u.Object["status"] = withoutNulls(c.expand(session, object.Status))
	}
	if kind == workshopEnvironmentKind {
		// The CRD requires the workshop's name and uid, which the service
		// does not read.
		if workshop, ok, _ := unstructured.NestedMap(u.Object, "status", "educates", "workshop"); ok {
			workshop["name"], _, _ = unstructured.NestedString(spec, "workshop", "name")
			workshop["uid"] = string(uuid.NewUUID())
			Expect(unstructured.SetNestedMap(u.Object, workshop, "status", "educates", "workshop")).To(Succeed())
		}
	}
	return u
}

// expand replaces the placeholders in the strings of value. A portal user
// the state names for the first time is bound to a new name, as the portal
// generates one.
func (c *testCluster) expand(session *golden.Session, value any) any {
	GinkgoHelper()
	switch v := value.(type) {
	case string:
		if strings.HasPrefix(v, "<portal-user:") {
			if _, ok := session.Value(v); !ok {
				session.Bind(v, string(uuid.NewUUID()))
			}
		}
		expanded, err := session.Expand(v)
		Expect(err).NotTo(HaveOccurred())
		return expanded
	case map[string]any:
		expanded := map[string]any{}
		for key, member := range v {
			expanded[key] = c.expand(session, member)
		}
		return expanded
	case []any:
		expanded := make([]any, len(v))
		for i, element := range v {
			expanded[i] = c.expand(session, element)
		}
		return expanded
	}
	return value
}

// withoutNulls drops the members of objects whose value is null, which the
// API server would drop itself.
func withoutNulls(value any) any {
	switch v := value.(type) {
	case map[string]any:
		kept := map[string]any{}
		for key, member := range v {
			if member != nil {
				kept[key] = withoutNulls(member)
			}
		}
		return kept
	case []any:
		kept := make([]any, len(v))
		for i, element := range v {
			kept[i] = withoutNulls(element)
		}
		return kept
	}
	return value
}

// createOrUpdate creates the resource, or replaces the spec, status and
// labels of the one of that name, and binds its uid. An object created
// again, after it was deleted, has a new uid, which its placeholder then
// stands for.
func (c *testCluster) createOrUpdate(session *golden.Session, u *unstructured.Unstructured) {
	GinkgoHelper()
	ctx := context.Background()
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(u.GroupVersionKind())
	err := c.client.Get(ctx, client.ObjectKey{Name: u.GetName()}, current)
	switch {
	case apierrors.IsNotFound(err):
		Expect(c.client.Create(ctx, u)).To(Succeed())
		current = u
	case err != nil:
		Expect(err).NotTo(HaveOccurred())
	default:
		current.SetLabels(u.GetLabels())
		current.Object["spec"] = u.Object["spec"]
		current.Object["status"] = u.Object["status"]
		Expect(c.client.Update(ctx, current)).To(Succeed())
	}
	placeholder := clusterUIDPlaceholder(c.name, current.GetKind(), current.GetName())
	session.Bind(placeholder, string(current.GetUID()))
}

// createTraining creates one training resource of the scenario's initial
// step on the cluster, for a spec that creates resources in an order of its
// own.
func (c *testCluster) createTraining(session *golden.Session, kind schema.GroupVersionKind, name string) {
	GinkgoHelper()
	resources := loadState("initial").Clusters[c.name]
	objects := map[schema.GroupVersionKind][]stateObject{
		trainingPortalKind:      resources.TrainingPortals,
		workshopEnvironmentKind: resources.WorkshopEnvironments,
		workshopSessionKind:     resources.WorkshopSessions,
	}[kind]
	for _, object := range objects {
		if object.Name == name {
			c.createOrUpdate(session, c.resolve(session, kind, object))
			return
		}
	}
	Fail("the initial step has no " + kind.Kind + " " + name + " on " + c.name)
}

// updateTraining changes the training resource of that kind and name.
func updateTraining(c *testCluster, kind schema.GroupVersionKind, name string, change func(map[string]any)) {
	GinkgoHelper()
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(kind)
	Expect(c.client.Get(context.Background(), client.ObjectKey{Name: name}, u)).To(Succeed())
	change(u.Object)
	Expect(c.client.Update(context.Background(), u)).To(Succeed())
}

// deleteTraining deletes the training resource of that kind and name.
func (c *testCluster) deleteTraining(kind schema.GroupVersionKind, name string) {
	GinkgoHelper()
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(kind)
	u.SetName(name)
	c.deleteObject(u)
}

func (c *testCluster) deleteObject(object client.Object) {
	GinkgoHelper()
	err := c.client.Delete(context.Background(), object)
	if !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

// removeTraining deletes every training resource of the cluster.
func (c *testCluster) removeTraining() {
	GinkgoHelper()
	for _, kind := range trainingKinds {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(kind)
		Expect(c.client.DeleteAllOf(context.Background(), u)).To(Succeed())
	}
}

// clusterUIDPlaceholder is the placeholder of the uid of an object on a
// cluster of the scenario.
func clusterUIDPlaceholder(cluster, kind, name string) string {
	return "<uid:" + cluster + "/" + kind + "/" + name + ">"
}
