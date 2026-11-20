package golden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math/big"
	"regexp"
	"slices"
	"strings"
)

// placeholderPattern finds placeholders such as <token:tenant-client>,
// <uid:cluster-a/ClientConfig/doomed-client#2> and <expires-at>.
var placeholderPattern = regexp.MustCompile(`<[a-z][a-z-]*(?::[^<>]+)?>`)

// placeholderKind says how a placeholder gets its value.
type placeholderKind int

const (
	// boundByTest placeholders stand for values the test knows, such as the
	// uid of an object it created; the test binds them before they are used.
	boundByTest placeholderKind = iota
	// boundOnSight placeholders stand for values the service makes, such as
	// a token: the first response to show one binds it, and every later
	// sighting must equal it.
	boundOnSight
	// boundPerFixture placeholders stand for one value within a fixture,
	// such as the expiry of the token a login issues.
	boundPerFixture
	// unbound placeholders stand for any value.
	unbound
)

func kindOf(placeholder string) (placeholderKind, error) {
	name, qualifier, _ := strings.Cut(strings.Trim(placeholder, "<>"), ":")
	switch name {
	case "uid", "server":
		return boundByTest, nil
	case "token":
		switch qualifier {
		case "":
			return unbound, nil
		case "expired", "foreign":
			return boundByTest, nil
		}
		return boundOnSight, nil
	case "session", "portal-user":
		return boundOnSight, nil
	case "expires-at":
		return boundPerFixture, nil
	case "generation", "activation-token", "certificate-authority-data":
		return unbound, nil
	}
	return 0, fmt.Errorf("unknown placeholder %s", placeholder)
}

// bindings holds the values placeholders stand for.
type bindings struct {
	values     map[string]string
	perFixture map[string]string
}

func newBindings() *bindings {
	return &bindings{values: map[string]string{}, perFixture: map[string]string{}}
}

func (b *bindings) clone() *bindings {
	return &bindings{values: maps.Clone(b.values), perFixture: maps.Clone(b.perFixture)}
}

// accept checks that value is what placeholder stands for, binding it when
// the placeholder is bound on sight or per fixture and not bound yet.
func (b *bindings) accept(placeholder, value string) error {
	kind, err := kindOf(placeholder)
	if err != nil {
		return err
	}
	values := b.values
	switch kind {
	case unbound:
		if value == "" {
			return fmt.Errorf("%s stands for an empty value", placeholder)
		}
		return nil
	case boundPerFixture:
		values = b.perFixture
	}

	if bound, ok := values[placeholder]; ok {
		if bound != value {
			return fmt.Errorf("%s is %q, not %q", placeholder, bound, value)
		}
		return nil
	}
	if kind == boundByTest {
		return fmt.Errorf("%s is not bound by the test", placeholder)
	}
	// Two placeholders of the same kind stand for different values: two
	// session labels are two sessions.
	prefix, _, _ := strings.Cut(placeholder, ":")
	for other, bound := range values {
		if bound == value && strings.HasPrefix(other, prefix) {
			return fmt.Errorf("%s and %s both stand for %q", placeholder, other, value)
		}
	}
	values[placeholder] = value
	return nil
}

// expand replaces every placeholder in text by its value.
func (b *bindings) expand(text string) (string, error) {
	var missing []string
	expanded := placeholderPattern.ReplaceAllStringFunc(text, func(placeholder string) string {
		if value, ok := b.values[placeholder]; ok {
			return value
		}
		missing = append(missing, placeholder)
		return placeholder
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("unbound placeholders in %q: %s", text, strings.Join(missing, ", "))
	}
	return expanded, nil
}

// decodeJSON decodes data keeping numbers as json.Number, so that integers
// compare exactly.
func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if decoder.More() {
		return nil, fmt.Errorf("data after the JSON value")
	}
	return value, nil
}

// match compares actual with expected, a value in placeholder form, binding
// placeholders as it goes. Objects must have the same members; arrays must
// hold the same elements in any order.
func (b *bindings) match(path string, expected, actual any) error {
	switch e := expected.(type) {
	case string:
		return b.matchString(path, e, actual)
	case json.Number:
		a, ok := actual.(json.Number)
		if !ok || !sameNumber(e, a) {
			return mismatch(path, expected, actual)
		}
		return nil
	case map[string]any:
		a, ok := actual.(map[string]any)
		if !ok {
			return mismatch(path, expected, actual)
		}
		expectedKeys := slices.Sorted(maps.Keys(e))
		if actualKeys := slices.Sorted(maps.Keys(a)); !slices.Equal(expectedKeys, actualKeys) {
			return fmt.Errorf("%s: members are %v, want %v", path, actualKeys, expectedKeys)
		}
		for _, key := range expectedKeys {
			if err := b.match(path+"."+key, e[key], a[key]); err != nil {
				return err
			}
		}
		return nil
	case []any:
		a, ok := actual.([]any)
		if !ok {
			return mismatch(path, expected, actual)
		}
		return b.matchMultiset(path, e, a)
	default:
		if expected != actual {
			return mismatch(path, expected, actual)
		}
		return nil
	}
}

// matchString compares actual with a string that may hold placeholders. A
// placeholder that is the whole string stands for any JSON scalar, as
// <expires-at> stands for an integer.
func (b *bindings) matchString(path, expected string, actual any) error {
	locations := placeholderPattern.FindAllStringIndex(expected, -1)
	if len(locations) == 1 && locations[0][0] == 0 && locations[0][1] == len(expected) {
		var value string
		switch a := actual.(type) {
		case string:
			value = a
		case json.Number:
			value = a.String()
		default:
			return mismatch(path, expected, actual)
		}
		if err := b.accept(expected, value); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		return nil
	}

	a, ok := actual.(string)
	if !ok {
		return mismatch(path, expected, actual)
	}
	if len(locations) == 0 {
		if a != expected {
			return mismatch(path, expected, actual)
		}
		return nil
	}

	var pattern strings.Builder
	pattern.WriteString("^")
	placeholders := make([]string, 0, len(locations))
	last := 0
	for _, location := range locations {
		pattern.WriteString(regexp.QuoteMeta(expected[last:location[0]]))
		pattern.WriteString("(.+?)")
		placeholders = append(placeholders, expected[location[0]:location[1]])
		last = location[1]
	}
	pattern.WriteString(regexp.QuoteMeta(expected[last:]))
	pattern.WriteString("$")

	groups := regexp.MustCompile(pattern.String()).FindStringSubmatch(a)
	if groups == nil {
		return mismatch(path, expected, actual)
	}
	for i, placeholder := range placeholders {
		if err := b.accept(placeholder, groups[i+1]); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

// matchMultiset compares two arrays as multisets, trying each pairing of
// elements in turn, since placeholders bound by one pairing decide whether
// the next can match.
func (b *bindings) matchMultiset(path string, expected, actual []any) error {
	if len(expected) != len(actual) {
		return fmt.Errorf("%s: %d elements, want %d: %s", path, len(actual), len(expected), render(actual))
	}
	used := make([]bool, len(actual))

	var pair func(i int) error
	pair = func(i int) error {
		if i == len(expected) {
			return nil
		}
		var firstErr error
		for j := range actual {
			if used[j] {
				continue
			}
			saved := b.clone()
			err := b.match(fmt.Sprintf("%s[%d]", path, i), expected[i], actual[j])
			if err == nil {
				used[j] = true
				if err = pair(i + 1); err == nil {
					return nil
				}
				used[j] = false
			}
			if firstErr == nil {
				firstErr = err
			}
			*b = *saved
		}
		return fmt.Errorf("%s: no element matches %s (%w)", path, render(expected[i]), firstErr)
	}
	return pair(0)
}

func sameNumber(a, b json.Number) bool {
	x, okX := new(big.Rat).SetString(a.String())
	y, okY := new(big.Rat).SetString(b.String())
	return okX && okY && x.Cmp(y) == 0
}

func mismatch(path string, expected, actual any) error {
	return fmt.Errorf("%s: got %s, want %s", path, render(actual), render(expected))
}

func render(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(data)
}
