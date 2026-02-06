package content

import (
	"errors"
	"html/template"
	"regexp"

	"github.com/microcosm-cc/bluemonday"
)

var (
	policy = bluemonday.UGCPolicy()
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
)

// Sanitize removes unsafe HTML from the input string using a strict policy.
// It is used for sanitizing user inputs like display names and messages.
func Sanitize(input string) string {
	return policy.Sanitize(input)
}

// Escape escapes special characters like "<" to become "&lt;".
// It matches the behavior of html/template and is safe for use in HTML attributes.
func Escape(input string) string {
	return template.HTMLEscapeString(input)
}

// ValidateUsername checks if the username contains only allowed characters
// (alphanumeric, dot, dash, underscore) and is not empty.
func ValidateUsername(username string) error {
	if username == "" {
		return errors.New("username cannot be empty")
	}
	if !usernameRegex.MatchString(username) {
		return errors.New("username contains invalid characters (allowed: alphanumeric, dot, dash, underscore)")
	}
	return nil
}
