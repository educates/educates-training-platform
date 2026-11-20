package deployer

import (
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/apply"
)

// TestOtherManagedSpecFieldsNamesWhatAnotherToolSet checks that the spec
// fields a field manager other than the CLI owns are listed with it,
// while the CLI's own fields, and the operator's status and finalizers,
// are not.
func TestOtherManagedSpecFieldsNamesWhatAnotherToolSet(t *testing.T) {
	when := metav1.NewTime(time.Date(2026, 11, 18, 21, 5, 0, 0, time.UTC))
	live := &unstructured.Unstructured{Object: map[string]interface{}{}}
	live.SetManagedFields([]metav1.ManagedFieldsEntry{
		{
			Manager:   apply.FieldManager,
			Operation: metav1.ManagedFieldsOperationApply,
			FieldsV1:  &metav1.FieldsV1{Raw: []byte(`{"f:spec":{"f:logLevel":{}}}`)},
		},
		{
			Manager:   "manager",
			Operation: metav1.ManagedFieldsOperationUpdate,
			FieldsV1:  &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:finalizers":{".":{},"v:\"educates.dev/finalizer\"":{}}}}`)},
		},
		{
			Manager:     "manager",
			Operation:   metav1.ManagedFieldsOperationUpdate,
			Subresource: "status",
			FieldsV1:    &metav1.FieldsV1{Raw: []byte(`{"f:status":{"f:phase":{}}}`)},
		},
		{
			Manager:   "kubectl-edit",
			Operation: metav1.ManagedFieldsOperationUpdate,
			Time:      &when,
			FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:spec":{"f:imagePrePuller":{".":{},"f:enabled":{}},` +
				`"f:images":{"f:overrides":{"k:{\"name\":\"session-manager\"}":{".":{},"f:image":{}}}}}}`)},
		},
	})

	want := []string{
		`spec.imagePrePuller.enabled, by kubectl-edit (Update at 2026-11-18 21:05:00 UTC)`,
		`spec.images.overrides[{"name":"session-manager"}].image, by kubectl-edit (Update at 2026-11-18 21:05:00 UTC)`,
	}
	if got := otherManagedSpecFields(live); !reflect.DeepEqual(got, want) {
		t.Errorf("otherManagedSpecFields =\n%q\nwant\n%q", got, want)
	}
}

func TestFieldPathsOfAFieldOwnedWhole(t *testing.T) {
	fields := map[string]interface{}{"f:network": map[string]interface{}{".": map[string]interface{}{}}}

	if got, want := fieldPaths(fields, "spec"), []string{"spec.network"}; !reflect.DeepEqual(got, want) {
		t.Errorf("fieldPaths = %q, want %q", got, want)
	}
}
