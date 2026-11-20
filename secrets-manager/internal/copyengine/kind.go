package copyengine

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
)

// Kind describes the kind of object the engine copies: which of its
// top-level fields a copy takes from the source, and how to read back what
// a field manager applied to an object of the kind.
type Kind struct {
	// GroupVersionKind of the source and its copies.
	GroupVersionKind schema.GroupVersionKind

	// WholeFields are the top-level fields a copy takes whole from the
	// source.
	WholeFields []string

	// KeyedFields are the top-level maps a copy takes from the source key
	// by key. The copy holds exactly the source's keys, and a key another
	// writer adds to a copy is removed.
	KeyedFields []string

	// Extract returns the configuration fieldManager applied to obj and
	// still owns, as unstructured content: the fields of the configuration
	// that set them, with obj's values.
	Extract func(obj *unstructured.Unstructured, fieldManager string) (map[string]any, error)
}

// Secret is the Kind of a Secret: a copy takes its type and its data.
var Secret = &Kind{
	GroupVersionKind: corev1.SchemeGroupVersion.WithKind("Secret"),
	WholeFields:      []string{"type"},
	KeyedFields:      []string{"data"},
	Extract:          extractSecret,
}

func extractSecret(obj *unstructured.Unstructured, fieldManager string) (map[string]any, error) {
	secret := &corev1.Secret{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, secret); err != nil {
		return nil, fmt.Errorf("reading Secret %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}
	applied, err := corev1ac.ExtractSecret(secret, fieldManager)
	if err != nil {
		return nil, fmt.Errorf("extracting what %s applied to Secret %s/%s: %w", fieldManager,
			obj.GetNamespace(), obj.GetName(), err)
	}
	return jsonContent(applied)
}

// jsonContent returns v as the unstructured content it marshals to.
func jsonContent(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	content := map[string]any{}
	if err := json.Unmarshal(raw, &content); err != nil {
		return nil, err
	}
	return content, nil
}
