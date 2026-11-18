package config

import (
	"encoding/json"
)

// Label is an Educates label: a name with an optional value, as clusters,
// training portals and workshops carry them in their resources.
type Label struct {
	Name string
	// Value is nil for a label without a value.
	Value *string
}

// MarshalJSON writes the label in the Educates list form, {name, value},
// leaving value out for a label without one, as the resource does.
func (l Label) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Name  string  `json:"name"`
		Value *string `json:"value,omitempty"`
	}{l.Name, l.Value})
}

// Labels is a list of Educates labels.
type Labels []Label

// Map returns the labels as selectors match them: a label without a value
// has the empty string as its value, and a later label of the same name
// wins.
func (l Labels) Map() map[string]string {
	labels := make(map[string]string, len(l))
	for _, label := range l {
		value := ""
		if label.Value != nil {
			value = *label.Value
		}
		labels[label.Name] = value
	}
	return labels
}

// MarshalJSON writes the labels as a list, [] when there are none.
func (l Labels) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("[]"), nil
	}
	return json.Marshal([]Label(l))
}
