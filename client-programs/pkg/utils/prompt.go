package utils

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// YesNoPrompt prompts the user for a yes/no answer to a question.
// It returns true if the user answers yes, false if the user answers no, and the default value if the user does not answer.
func YesNoPrompt(labels []string, def bool) bool {
	choices := "Y/n"
	if !def {
		choices = "y/N"
	}

    // 1. Construct the prompt string
	// Join all strings with a newline
	labelText := strings.Join(labels, "\n")

	// Append the choices to the very end
	// Result example: "Line1\nLine2 (Y/n) "
	fullPrompt := fmt.Sprintf("%s (%s) ", labelText, choices)

	r := bufio.NewReader(os.Stdin)
	for {
		fmt.Fprintf(os.Stderr, "%s",fullPrompt)
		s, _ := r.ReadString('\n')
		s = strings.TrimSpace(s)
		if s == "" {
			return def
		}
		s = strings.ToLower(s)
		if s == "y" || s == "yes" {
			return true
		}
		if s == "n" || s == "no" {
			return false
		}
	}
}
