package config

import (
	"fmt"
	"regexp"

	"github.com/xeipuuv/gojsonschema"
)

// prePulledImageEntry matches the field of an imagePrePuller.images entry.
var prePulledImageEntry = regexp.MustCompile(`^imagePrePuller\.images\.\d+$`)

// schemaErrorLines renders the schema validation errors one line each, as
// "<field>: <description>". An error the narrow kinds' schemas can only
// report in gojsonschema's generic terms is rewritten to say what to
// write instead.
func schemaErrorLines(errs []gojsonschema.ResultError) []string {
	// The imagePrePuller.images entries giving both or neither of name
	// and image, which fail the entry schema's oneOf.
	notExactlyOne := map[string]bool{}
	for _, e := range errs {
		if e.Type() == "number_one_of" && prePulledImageEntry.MatchString(e.Field()) {
			notExactlyOne[e.Field()] = true
		}
	}

	lines := make([]string, 0, len(errs))
	for _, e := range errs {
		b, isBool := e.Value().(bool)
		switch {
		case isBool && e.Field() == "imagePrePuller" && e.Type() == "invalid_type":
			lines = append(lines, "imagePrePuller: "+ImagePrePullerBooleanForm(b))
		case notExactlyOne[e.Field()] && e.Type() == "number_one_of":
			lines = append(lines, e.Field()+": must set exactly one of name and image")
		case notExactlyOne[e.Field()] && e.Type() == "required":
			// The name or image the oneOf branch closest to the entry
			// requires, which the line above already covers.
		default:
			lines = append(lines, fmt.Sprintf("%s: %s", e.Field(), e.Description()))
		}
	}
	return lines
}

// ImagePrePullerBooleanForm describes an imagePrePuller given as a
// boolean, a form the narrow kinds no longer take, and names the object
// form to write in its place.
func ImagePrePullerBooleanForm(value bool) string {
	return fmt.Sprintf("takes an object, not a boolean; set imagePrePuller.enabled instead, writing \"imagePrePuller: {enabled: %t}\" in place of \"imagePrePuller: %t\"", value, value)
}
