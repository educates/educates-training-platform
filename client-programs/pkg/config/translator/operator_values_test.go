package translator

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// TestOperatorConfigFromChartValuesInvertsTheTranslation checks that the
// operator block recovered from the chart values a config's operator block
// translates to is that block, with nothing left unmapped.
func TestOperatorConfigFromChartValuesInvertsTheTranslation(t *testing.T) {
	cases := []v1alpha1.LocalOperatorConfig{
		{},
		{LogLevel: "debug"},
		{
			Image:            v1alpha1.OperatorImage{Repository: "localhost:5001/educates-operator", Tag: "latest", PullPolicy: "Always"},
			ImagePullSecrets: []string{"one", "two"},
			LogLevel:         "info",
		},
	}

	for _, op := range cases {
		got, unmapped := OperatorConfigFromChartValues(jsonValues(t, operatorChartValuesFor(op)))

		if !reflect.DeepEqual(got, op) {
			t.Errorf("OperatorConfigFromChartValues = %+v, want %+v", got, op)
		}
		if len(unmapped) != 0 {
			t.Errorf("unmapped = %q, want none", unmapped)
		}
	}
}

// TestOperatorConfigFromChartValuesNamesWhatItCannotMap checks that chart
// values the operator block has no field for are named by dotted path.
func TestOperatorConfigFromChartValuesNamesWhatItCannotMap(t *testing.T) {
	values := map[string]interface{}{
		"image":            map[string]interface{}{"repository": "r", "digest": "sha256:x"},
		"imagePullSecrets": []interface{}{map[string]interface{}{"name": "ok"}, map[string]interface{}{"name": "x", "extra": "y"}},
		"rbac":             map[string]interface{}{"clusterAdmin": true},
		"logLevel":         3,
	}

	got, unmapped := OperatorConfigFromChartValues(values)

	if got.Image.Repository != "r" || !reflect.DeepEqual(got.ImagePullSecrets, []string{"ok"}) {
		t.Errorf("OperatorConfigFromChartValues = %+v, want the mappable values", got)
	}
	want := []string{"image.digest", "imagePullSecrets[1]", "logLevel", "rbac"}
	if !reflect.DeepEqual(unmapped, want) {
		t.Errorf("unmapped = %q, want %q", unmapped, want)
	}
}

// jsonValues returns values as a Helm release records them: decoded from
// JSON, with []interface{} lists.
func jsonValues(t *testing.T, values map[string]interface{}) map[string]interface{} {
	t.Helper()
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
