package config

import (
	"slices"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
)

// Tenant is a tenant, projected from its TenantConfig: the clusters and the
// training portals on them that its clients may offer workshops from.
type Tenant struct {
	Name string
	// Clusters selects the clusters whose portals the tenant reaches.
	Clusters Selector
	// Portals selects the portals, on those clusters, the tenant reaches.
	Portals Selector
}

// ReachesCluster reports whether the tenant reaches the portals of the
// cluster of that name and those Educates labels.
func (t Tenant) ReachesCluster(name string, labels Labels) bool {
	return t.Clusters.Matches(name, labels)
}

// ReachesPortal reports whether the tenant reaches the portal of that name
// and those Educates labels, on a cluster it reaches.
func (t Tenant) ReachesPortal(name string, labels Labels) bool {
	return t.Portals.Matches(name, labels)
}

// Selector selects resources by name and by their Educates labels. A
// resource must match both, and an empty selector matches every resource.
type Selector struct {
	// Names are glob patterns, one of which a name must match. None means
	// any name.
	Names []string
	// Labels are labels a resource must have, with these values.
	Labels map[string]string
	// Requirements are conditions on labels, all of which a resource must
	// meet.
	Requirements []Requirement
}

// Requirement is a condition on one label.
type Requirement struct {
	Key string
	// Operator is In, NotIn, Exists or DoesNotExist.
	Operator string
	Values   []string
}

// The operators of a Requirement.
const (
	OperatorIn           = "In"
	OperatorNotIn        = "NotIn"
	OperatorExists       = "Exists"
	OperatorDoesNotExist = "DoesNotExist"
)

// Matches reports whether the resource of that name and those Educates
// labels matches the selector. Labels are matched as a map, a label without
// a value having the empty string as its value; resources' Kubernetes
// metadata labels are never looked at.
func (s Selector) Matches(name string, labels Labels) bool {
	if len(s.Names) > 0 && !slices.ContainsFunc(s.Names, func(pattern string) bool {
		return globMatches(pattern, name)
	}) {
		return false
	}
	values := labels.Map()
	for key, want := range s.Labels {
		if value, ok := values[key]; !ok || value != want {
			return false
		}
	}
	for _, requirement := range s.Requirements {
		if !requirement.matches(values) {
			return false
		}
	}
	return true
}

func (r Requirement) matches(labels map[string]string) bool {
	value, ok := labels[r.Key]
	switch r.Operator {
	case OperatorIn:
		return ok && slices.Contains(r.Values, value)
	case OperatorNotIn:
		return !ok || !slices.Contains(r.Values, value)
	case OperatorExists:
		return ok
	case OperatorDoesNotExist:
		return !ok
	}
	return false
}

func projectTenant(config *lookupv1beta1.TenantConfig) Tenant {
	return Tenant{
		Name:     config.Name,
		Clusters: projectSelector(config.Spec.Clusters),
		Portals:  projectSelector(config.Spec.Portals),
	}
}

func projectSelector(selector *lookupv1beta1.ResourceSelector) Selector {
	var s Selector
	if selector == nil {
		return s
	}
	if selector.NameSelector != nil {
		s.Names = slices.Clone(selector.NameSelector.MatchNames)
	}
	if selector.LabelSelector != nil {
		s.Labels = selector.LabelSelector.MatchLabels
		for _, expression := range selector.LabelSelector.MatchExpressions {
			s.Requirements = append(s.Requirements, Requirement{
				Key:      expression.Key,
				Operator: expression.Operator,
				Values:   slices.Clone(expression.Values),
			})
		}
	}
	return s
}
