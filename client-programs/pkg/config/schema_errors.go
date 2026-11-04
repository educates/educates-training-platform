package config

import (
	"fmt"
	"regexp"

	"github.com/xeipuuv/gojsonschema"
)

// rootField is the field gojsonschema gives an error on the document's
// root.
const rootField = "(root)"

// schemaErrorRule rewrites a schema error gojsonschema can only report in
// generic terms into a line that says what to write instead. gojsonschema
// reports a failing oneOf, if/then or allOf as an error of its own beside
// the errors of the subschemas that failed, so a rule also names those
// companion errors, which its line covers.
type schemaErrorRule struct {
	// field matches the field of the error that triggers the rule.
	field *regexp.Regexp
	// errorType is the gojsonschema type of the error that triggers the
	// rule.
	errorType string
	// when, if set, narrows the trigger on the error's value or details.
	when func(gojsonschema.ResultError) bool
	// line writes the line the triggering error reports, field first.
	line func(gojsonschema.ResultError) string
	// drops are the companion errors the line covers.
	drops []companionError
}

// companionError names an error that a failure reports beside the error
// triggering a rule: an error of errorType on the triggering error's own
// field or, with child set, on that child of it.
type companionError struct {
	errorType string
	child     string
}

// field returns the field of the companion error of an error on trigger.
func (c companionError) field(trigger string) string {
	switch {
	case c.child == "":
		return trigger
	case trigger == rootField:
		return c.child
	default:
		return trigger + "." + c.child
	}
}

// schemaErrorRules are the errors the narrow kinds' schemas can only
// report in gojsonschema's generic terms.
var schemaErrorRules = []schemaErrorRule{
	{
		// An imagePrePuller given as a boolean, a form the narrow kinds
		// no longer take.
		field:     regexp.MustCompile(`^imagePrePuller$`),
		errorType: "invalid_type",
		when:      valueIsBool,
		line: func(e gojsonschema.ResultError) string {
			return "imagePrePuller: " + ImagePrePullerBooleanFormMessage(e.Value().(bool))
		},
	},
	{
		// An imagePrePuller.images entry giving both or neither of name
		// and image fails the entry schema's oneOf, whose branches each
		// require one of them.
		field:     regexp.MustCompile(`^imagePrePuller\.images\.\d+$`),
		errorType: "number_one_of",
		line:      fieldLine("must set exactly one of name and image"),
		drops:     []companionError{{errorType: "required"}},
	},
	{
		// An imagePrePuller switched on with nothing to pull fails the
		// block's if/then, whose then branch requires images with at
		// least one entry.
		field:     regexp.MustCompile(`^imagePrePuller$`),
		errorType: "condition_then",
		line:      fieldLine("with enabled true, its default, and includeDefaultImages false, images must list at least one entry"),
		drops: []companionError{
			{errorType: "required"},
			{errorType: "array_min_items", child: "images"},
		},
	},
}

// valueIsBool reports whether the error's value is a boolean.
func valueIsBool(e gojsonschema.ResultError) bool {
	_, ok := e.Value().(bool)
	return ok
}

// fieldLine returns a line writer that leads sentence with the error's
// field.
func fieldLine(sentence string) func(gojsonschema.ResultError) string {
	return func(e gojsonschema.ResultError) string {
		return e.Field() + ": " + sentence
	}
}

// triggers reports whether e is the error that triggers the rule.
func (r schemaErrorRule) triggers(e gojsonschema.ResultError) bool {
	return e.Type() == r.errorType && r.field.MatchString(e.Field()) && (r.when == nil || r.when(e))
}

// triggeredRule returns the rule e triggers, or nil when it triggers none.
func triggeredRule(e gojsonschema.ResultError) *schemaErrorRule {
	for i := range schemaErrorRules {
		if schemaErrorRules[i].triggers(e) {
			return &schemaErrorRules[i]
		}
	}
	return nil
}

// errorKey identifies an error by its field and type.
type errorKey struct {
	field, errorType string
}

// SchemaErrorLines renders the errors of validating a configuration
// against its kind's schema one line each, as "<field>: <description>". An
// error that triggers one of schemaErrorRules is written as the rule's line
// instead, and the companion errors the rule names are left out.
func SchemaErrorLines(errs []gojsonschema.ResultError) []string {
	covered := map[errorKey]bool{}
	for _, e := range errs {
		if r := triggeredRule(e); r != nil {
			for _, c := range r.drops {
				covered[errorKey{c.field(e.Field()), c.errorType}] = true
			}
		}
	}

	lines := make([]string, 0, len(errs))
	for _, e := range errs {
		if r := triggeredRule(e); r != nil {
			lines = append(lines, r.line(e))
			continue
		}
		if covered[errorKey{e.Field(), e.Type()}] {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: %s", e.Field(), e.Description()))
	}
	return lines
}

// ImagePrePullerBooleanFormMessage returns the message for an
// imagePrePuller given as a boolean, a form the narrow kinds no longer
// take: it names the object form to write in its place.
func ImagePrePullerBooleanFormMessage(value bool) string {
	return fmt.Sprintf("takes an object, not a boolean; set imagePrePuller.enabled instead, writing \"imagePrePuller: {enabled: %t}\" in place of \"imagePrePuller: %t\"", value, value)
}
