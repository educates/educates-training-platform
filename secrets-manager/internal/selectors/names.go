// Package selectors decides whether a namespace, a Secret or a service
// account matches the selectors of a secrets.educates.dev rule. Its
// functions are pure: they look only at the object and the rule, and are
// shared by all four kinds. A rule's selectors for one kind of object go
// through New, its target namespaces through Namespaces or NamespacesOf,
// and a SecretInjector rule's source Secrets and service accounts through
// SourceSecrets and ServiceAccounts; a selector that is invalid matches
// nothing, and its Err says why.
package selectors

import (
	"regexp"
	"strings"
	"sync"
)

// MatchNames reports whether name passes the patterns of a matchNames
// list. A pattern with a leading ! excludes the names it matches; any other
// pattern includes them. Name must match an including pattern, when the
// list has any, and no excluding one. Patterns are globs as Python's
// fnmatch reads them: * matches any run of characters, ? any one
// character, [seq] any character in seq and [!seq] any character not in
// it, and the whole name must match. Matching is case sensitive.
func MatchNames(patterns []string, name string) bool {
	included, hasInclusions := false, false
	for _, pattern := range patterns {
		if excluded, ok := strings.CutPrefix(pattern, "!"); ok {
			if globMatches(excluded, name) {
				return false
			}
			continue
		}
		hasInclusions = true
		if !included && globMatches(pattern, name) {
			included = true
		}
	}
	return included || !hasInclusions
}

// globMatches reports whether name matches the glob pattern. A [ without
// its closing ] is an ordinary character.
func globMatches(pattern, name string) bool {
	return compileGlob(pattern).MatchString(name)
}

var (
	globsMu sync.Mutex
	globs   = map[string]*regexp.Regexp{}
)

// neverMatches matches no name, for a pattern no regular expression can
// stand for, such as one with a reversed range in a set.
var neverMatches = regexp.MustCompile(`[^\x00-\x{10FFFF}]`)

func compileGlob(pattern string) *regexp.Regexp {
	globsMu.Lock()
	defer globsMu.Unlock()
	if re, ok := globs[pattern]; ok {
		return re
	}
	re, err := regexp.Compile(translateGlob(pattern))
	if err != nil {
		re = neverMatches
	}
	globs[pattern] = re
	return re
}

// translateGlob turns a glob pattern into a regular expression matching
// the same names.
func translateGlob(pattern string) string {
	var re strings.Builder
	re.WriteString(`(?s)\A`)
	chars := []rune(pattern)
	for i := 0; i < len(chars); {
		c := chars[i]
		i++
		switch c {
		case '*':
			for i < len(chars) && chars[i] == '*' {
				i++
			}
			re.WriteString(`.*`)
		case '?':
			re.WriteString(`.`)
		case '[':
			set, next, ok := globSet(chars, i)
			if !ok {
				re.WriteString(`\[`)
				continue
			}
			re.WriteString(set)
			i = next
		default:
			re.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	re.WriteString(`\z`)
	return re.String()
}

// globSet translates the set starting at chars[start], right after its [,
// returning the character class, the index after the set's ], and false
// when the set is not closed.
func globSet(chars []rune, start int) (string, int, bool) {
	j := start
	if j < len(chars) && chars[j] == '!' {
		j++
	}
	// A ] right after the [ or [! belongs to the set.
	if j < len(chars) && chars[j] == ']' {
		j++
	}
	for j < len(chars) && chars[j] != ']' {
		j++
	}
	if j >= len(chars) {
		return "", 0, false
	}

	// The set holds at least one character, since a ] right after the [
	// or [! is one.
	members := chars[start:j]
	negated := members[0] == '!'
	if negated {
		members = members[1:]
	}

	var class strings.Builder
	class.WriteString("[")
	if negated {
		class.WriteString("^")
	}
	for k, c := range members {
		switch {
		case c == '-' && k > 0 && k < len(members)-1:
			// A - between two characters makes a range.
			class.WriteRune('-')
		case c < 0x80 && !isAlphanumeric(c):
			// Any other ASCII punctuation stands for itself.
			class.WriteRune('\\')
			class.WriteRune(c)
		default:
			class.WriteRune(c)
		}
	}
	class.WriteString("]")
	return class.String(), j + 1, true
}

func isAlphanumeric(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
