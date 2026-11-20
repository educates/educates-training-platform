package inventory

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/educates/educates-training-platform/lookup-service/internal/config"
)

// The labels the training portal sets on the resources it creates, naming
// the resources they belong to.
const (
	PortalNameLabel      = "training.educates.dev/portal.name"
	PortalUIDLabel       = "training.educates.dev/portal.uid"
	PortalWorkshopLabel  = "training.educates.dev/portal.workshop"
	EnvironmentNameLabel = "training.educates.dev/environment.name"
	EnvironmentUIDLabel  = "training.educates.dev/environment.uid"
)

// portalRecord is a TrainingPortal as the inventory keeps it.
type portalRecord struct {
	name, uid              string
	generation             int64
	labels                 config.Labels
	url                    string
	namespace              string
	robotCredentialsSecret string
	phase                  string
	capacity               int64
}

func projectPortal(u *unstructured.Unstructured) portalRecord {
	labels, _, _ := unstructured.NestedSlice(u.Object, "spec", "portal", "labels")
	return portalRecord{
		name:                   u.GetName(),
		uid:                    string(u.GetUID()),
		generation:             u.GetGeneration(),
		labels:                 config.ProjectLabels(labels),
		url:                    nestedString(u, "status", "educates", "url"),
		namespace:              nestedString(u, "status", "educates", "namespace"),
		robotCredentialsSecret: nestedString(u, "status", "educates", "credentialSecrets", "robot"),
		phase:                  nestedString(u, "status", "educates", "phase"),
		capacity:               nestedInt(u, "spec", "portal", "sessions", "maximum"),
	}
}

// environmentRecord is a WorkshopEnvironment as the inventory keeps it.
type environmentRecord struct {
	name, uid   string
	portal      string
	portalUID   string
	generation  int64
	workshop    string
	title       string
	description string
	labels      config.Labels
	capacity    *int64
	reserved    int64
	phase       string
}

func projectEnvironment(u *unstructured.Unstructured) environmentRecord {
	objectLabels := u.GetLabels()
	// The portal names an environment's workshop by the alias it offers
	// the workshop under, which the label holds.
	workshop := objectLabels[PortalWorkshopLabel]
	if workshop == "" {
		workshop = nestedString(u, "spec", "workshop", "name")
	}
	labels, _, _ := unstructured.NestedSlice(u.Object, "status", "educates", "workshop", "spec", "labels")
	record := environmentRecord{
		name:        u.GetName(),
		uid:         string(u.GetUID()),
		portal:      objectLabels[PortalNameLabel],
		portalUID:   objectLabels[PortalUIDLabel],
		generation:  nestedInt(u, "status", "educates", "workshop", "generation"),
		workshop:    workshop,
		title:       nestedString(u, "status", "educates", "workshop", "spec", "title"),
		description: nestedString(u, "status", "educates", "workshop", "spec", "description"),
		labels:      config.ProjectLabels(labels),
		reserved:    nestedInt(u, "status", "educates", "reserved"),
		phase:       nestedString(u, "status", "educates", "phase"),
	}
	if capacity, ok := nestedNumber(u, "status", "educates", "capacity"); ok {
		record.capacity = &capacity
	}
	return record
}

// sessionRecord is a WorkshopSession as the inventory keeps it.
type sessionRecord struct {
	name           string
	portal         string
	portalUID      string
	environment    string
	environmentUID string
	generation     int64
	workshop       string
	phase          string
	user           string
}

func projectSession(u *unstructured.Unstructured) sessionRecord {
	labels := u.GetLabels()
	return sessionRecord{
		name:           u.GetName(),
		portal:         labels[PortalNameLabel],
		portalUID:      labels[PortalUIDLabel],
		environment:    labels[EnvironmentNameLabel],
		environmentUID: labels[EnvironmentUIDLabel],
		generation:     u.GetGeneration(),
		workshop:       nestedString(u, "spec", "workshop", "name"),
		phase:          nestedString(u, "status", "educates", "phase"),
		user:           nestedString(u, "status", "educates", "user"),
	}
}

func nestedString(u *unstructured.Unstructured, fields ...string) string {
	value, _, _ := unstructured.NestedFieldNoCopy(u.Object, fields...)
	s, _ := value.(string)
	return s
}

// nestedInt returns the integer at fields, 0 when there is none.
func nestedInt(u *unstructured.Unstructured, fields ...string) int64 {
	n, _ := nestedNumber(u, fields...)
	return n
}

// nestedNumber returns the integer at fields, and whether there is one. JSON
// decoding gives integers as int64, or as float64 for a value written with
// a fraction or exponent.
func nestedNumber(u *unstructured.Unstructured, fields ...string) (int64, bool) {
	value, _, _ := unstructured.NestedFieldNoCopy(u.Object, fields...)
	switch n := value.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		return int64(n), true
	}
	return 0, false
}
