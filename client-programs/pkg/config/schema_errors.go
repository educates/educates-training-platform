package config

import (
	"fmt"

	"github.com/xeipuuv/gojsonschema"
)

// schemaErrorLines renders the schema validation errors one line each, as
// "<field>: <description>". An error the narrow kinds' schemas can only
// report in gojsonschema's generic terms is rewritten to say what to
// write instead.
func schemaErrorLines(errs []gojsonschema.ResultError) []string {
	lines := make([]string, 0, len(errs))
	for _, e := range errs {
		if b, ok := e.Value().(bool); ok && e.Field() == "imagePrePuller" && e.Type() == "invalid_type" {
			lines = append(lines, "imagePrePuller: "+ImagePrePullerBooleanForm(b))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", e.Field(), e.Description()))
	}
	return lines
}

// ImagePrePullerBooleanForm describes an imagePrePuller given as a
// boolean, a form the narrow kinds no longer take, and names the object
// form to write in its place.
func ImagePrePullerBooleanForm(value bool) string {
	return fmt.Sprintf("takes an object, not a boolean; set imagePrePuller.enabled instead, writing \"imagePrePuller: {enabled: %t}\" in place of \"imagePrePuller: %t\"", value, value)
}
