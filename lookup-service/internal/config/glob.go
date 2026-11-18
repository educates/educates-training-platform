package config

import (
	"regexp"
	"strings"
	"sync"
)

// globMatches reports whether name matches the glob pattern as Python's
// fnmatch matches it, which the service always used: * matches any run of
// characters, ? any one character, [seq] any character in seq and [!seq]
// any character not in it, and the whole name must match. A [ without its
// closing ] is an ordinary character.
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
