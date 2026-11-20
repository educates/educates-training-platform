package selectors

import (
	"testing"
)

func TestMatchNames(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		input    string
		want     bool
	}{
		{"an exact name matches itself", []string{"apps"}, "apps", true},
		{"an exact name matches no other", []string{"apps"}, "apps-dev", false},
		{"any of several names", []string{"apps", "web"}, "web", true},
		{"a star matches any run of characters", []string{"team-*"}, "team-a-dev", true},
		{"a star needs the rest of the pattern", []string{"team-*"}, "teams", false},
		{"a question mark matches one character", []string{"ns-?"}, "ns-a", true},
		{"a question mark matches exactly one", []string{"ns-?"}, "ns-ab", false},
		{"a set matches a character in it", []string{"ns-[ab]"}, "ns-b", true},
		{"a set matches no character outside it", []string{"ns-[ab]"}, "ns-c", false},
		{"a negated set matches a character outside it", []string{"ns-[!ab]"}, "ns-c", true},
		{"a range in a set", []string{"ns-[0-9]"}, "ns-7", true},
		{"an unclosed set is an ordinary bracket", []string{"ns-[a"}, "ns-[a", true},
		{"the whole name must match", []string{"app"}, "my-app", false},
		{"matching is case sensitive", []string{"Apps"}, "apps", false},
		{"an exclusion rejects a name it matches", []string{"!kube-*"}, "kube-system", false},
		{"with only exclusions, any other name passes", []string{"!kube-*"}, "default", true},
		{"an exclusion wins over an inclusion", []string{"team-*", "!team-b"}, "team-b", false},
		{"an inclusion with an exclusion it does not hit", []string{"team-*", "!team-b"}, "team-a", true},
		{"a name no inclusion matches fails despite exclusions", []string{"team-*", "!team-b"}, "other", false},
		{"no patterns pass every name", nil, "anything", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := MatchNames(test.patterns, test.input); got != test.want {
				t.Errorf("MatchNames(%q, %q) = %v, want %v", test.patterns, test.input, got, test.want)
			}
		})
	}
}
