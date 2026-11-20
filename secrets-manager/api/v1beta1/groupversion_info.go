// Package v1beta1 holds the Go types of the secrets.educates.dev/v1beta1
// resources, which the secrets-manager owns: SecretCopier, SecretExporter,
// SecretImporter and SecretInjector. The CustomResourceDefinitions in the
// secrets-manager subchart's crds/ directory are the schema; these types
// mirror them.
//
// +kubebuilder:object:generate=true
// +groupName=secrets.educates.dev
package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is the group and version of the secrets-manager's
	// resources.
	GroupVersion = schema.GroupVersion{Group: "secrets.educates.dev", Version: "v1beta1"}

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
