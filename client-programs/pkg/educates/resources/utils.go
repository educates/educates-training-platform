package resources

import (
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)


func ExtractWorkshopNamesFromTrainingPortalResource(trainingPortal *unstructured.Unstructured) ([]string, error) {
	workshops, _, err := unstructured.NestedSlice(trainingPortal.Object, "spec", "workshops")
	if err != nil {
		return nil, errors.Wrap(err, "unable to retrieve workshops from training portal")
	}

	names := []string{}
	seen := map[string]struct{}{}

	for _, item := range workshops {
		object, ok := item.(map[string]interface{})
		if !ok {
			return nil, errors.Errorf("invalid workshop reference in training portal %q", trainingPortal.GetName())
		}

		name, ok := object["name"].(string)
		if !ok || name == "" {
			return nil, errors.Errorf("invalid workshop reference in training portal %q", trainingPortal.GetName())
		}

		if _, exists := seen[name]; exists {
			continue
		}

		seen[name] = struct{}{}
		names = append(names, name)
	}

	return names, nil
}
