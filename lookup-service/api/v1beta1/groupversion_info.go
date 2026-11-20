// Package v1beta1 holds the Go types of the lookup.educates.dev/v1beta1
// resources, which the Lookup Service owns. The CustomResourceDefinitions in
// the lookup-service subchart's crds/ directory are the schema; these types
// mirror the fields the service reads.
//
// +kubebuilder:object:generate=true
// +groupName=lookup.educates.dev
package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is the group and version of the Lookup Service's resources.
	GroupVersion = schema.GroupVersion{Group: "lookup.educates.dev", Version: "v1beta1"}

	// SchemeBuilder collects the functions that add this group and
	// version's types to a scheme. It depends on apimachinery only, so that
	// the package stays cheap to import.
	SchemeBuilder = runtime.NewSchemeBuilder()

	// AddToScheme adds the types in this group and version to a scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// register queues object types for registration under this group and
// version. Each *_types.go file calls it from init().
func register(objs ...runtime.Object) {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, objs...)
		metav1.AddToGroupVersion(s, GroupVersion)
		return nil
	})
}
