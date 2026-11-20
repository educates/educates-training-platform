package inventory_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/educates/educates-training-platform/lookup-service/internal/inventory"
)

const (
	clusterName     = "cluster-a"
	portalName      = "portal-a"
	environmentName = "portal-a-w01"
)

func environmentObject() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name":   environmentName,
			"labels": map[string]any{inventory.PortalNameLabel: portalName},
		},
		"status": map[string]any{"educates": map[string]any{"phase": inventory.EnvironmentRunning}},
	}}
}

func sessionObject(name, phase string) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name": name,
			"labels": map[string]any{
				inventory.PortalNameLabel:      portalName,
				inventory.EnvironmentNameLabel: environmentName,
			},
		},
		"status": map[string]any{"educates": map[string]any{"phase": phase}},
	}}
}

// syncedInventory returns an inventory whose one cluster has synced with one
// environment, and the cluster's feed.
func syncedInventory(t *testing.T, sessions ...*unstructured.Unstructured) (*inventory.Inventory, *inventory.Feed) {
	t.Helper()
	inv := inventory.New()
	inv.SetCluster(clusterName, inventory.ClusterInfo{})
	feed := inv.Attach(clusterName)
	feed.SetEnvironment(environmentObject())
	for _, session := range sessions {
		feed.SetSession(session)
	}
	feed.MarkSynced()
	return inv, feed
}

// environment returns the environment as the inventory shows it now.
func environment(t *testing.T, inv *inventory.Inventory) *inventory.Environment {
	t.Helper()
	cluster, ok := inv.Snapshot().Cluster(clusterName)
	if !ok {
		t.Fatal("the inventory has no cluster")
	}
	portal, ok := cluster.Portal(portalName)
	if !ok {
		t.Fatal("the inventory has no portal")
	}
	e, ok := portal.Environment(environmentName)
	if !ok {
		t.Fatal("the inventory has no environment")
	}
	return e
}

// expectTaken fails unless the environment shows that many sessions and
// requests in flight.
func expectTaken(t *testing.T, inv *inventory.Inventory, sessions, inFlight int) {
	t.Helper()
	e := environment(t, inv)
	if e.Allocated != sessions || e.InFlight != inFlight {
		t.Fatalf("the environment has %d sessions taken and %d requests in flight, expected %d and %d",
			e.Allocated, e.InFlight, sessions, inFlight)
	}
}

func TestAnUnansweredRequestCountsUntilTheWatchShowsASessionItMayHaveGiven(t *testing.T) {
	inv, feed := syncedInventory(t, sessionObject("s1", inventory.SessionAllocated))
	request := inv.StartRequest(environment(t, inv))
	request.Unanswered()
	expectTaken(t, inv, 1, 1)

	// A change to a session taken before the request tells nothing of it.
	feed.SetSession(sessionObject("s1", inventory.SessionStopping))
	expectTaken(t, inv, 1, 1)
	// Nor does a reserved session, which no one has taken.
	feed.SetSession(sessionObject("s2", inventory.SessionAvailable))
	expectTaken(t, inv, 1, 1)

	feed.SetSession(sessionObject("s2", inventory.SessionAllocating))
	expectTaken(t, inv, 2, 0)
}

func TestAnUnansweredRequestEndsAtOnceWhenItsSessionShowedWhileItWaited(t *testing.T) {
	inv, feed := syncedInventory(t)
	request := inv.StartRequest(environment(t, inv))
	feed.SetSession(sessionObject("s1", inventory.SessionAllocating))
	expectTaken(t, inv, 1, 1)

	request.Unanswered()
	expectTaken(t, inv, 1, 0)
}

func TestASessionEndsOnlyTheRequestThatGaveIt(t *testing.T) {
	inv, feed := syncedInventory(t)
	answered := inv.StartRequest(environment(t, inv))
	unanswered := inv.StartRequest(environment(t, inv))

	// The answered request's session shows before its portal's answer is
	// recorded, while the other request still waits.
	feed.SetSession(sessionObject("s1", inventory.SessionAllocating))
	answered.Placed("s1")
	expectTaken(t, inv, 1, 1)

	unanswered.Unanswered()
	expectTaken(t, inv, 1, 1)

	feed.SetSession(sessionObject("s2", inventory.SessionAllocating))
	expectTaken(t, inv, 2, 0)
}

func TestASessionAPlacedRequestNamesLeavesAnUnansweredRequestCounting(t *testing.T) {
	inv, feed := syncedInventory(t)
	unanswered := inv.StartRequest(environment(t, inv))
	unanswered.Unanswered()
	placed := inv.StartRequest(environment(t, inv))
	placed.Placed("s1")
	expectTaken(t, inv, 0, 2)

	feed.SetSession(sessionObject("s1", inventory.SessionAllocating))
	expectTaken(t, inv, 1, 1)
}

func TestAFailedRequestStopsCountingAtOnce(t *testing.T) {
	inv, _ := syncedInventory(t)
	request := inv.StartRequest(environment(t, inv))
	expectTaken(t, inv, 0, 1)
	request.Failed()
	expectTaken(t, inv, 0, 0)
}
