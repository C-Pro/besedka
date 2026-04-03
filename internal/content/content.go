package content

import (
	"bytes"
	"errors"
	"html/template"
	"regexp"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
)

var (
	policy        = bluemonday.StrictPolicy()
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

	markdownPolicy = func() *bluemonday.Policy {
		p := bluemonday.NewPolicy()
		p.AllowElements("p", "br", "strong", "b", "em", "i", "a", "code", "pre", "blockquote", "ul", "ol", "li", "h1", "h2", "h3", "h4", "h5", "h6")
		p.AllowAttrs("href").OnElements("a")
		p.RequireParseableURLs(true)
		p.AllowURLSchemes("http", "https", "mailto")
		return p
	}()

	mdParser = goldmark.New(
		goldmark.WithExtensions(extension.Linkify),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithXHTML(),
		),
	)
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

// FormatMessage converts markdown to HTML, then sanitizes the result.
// Order matters: goldmark first (markdown→HTML), bluemonday second (sanitize HTML).
func FormatMessage(input string) string {
	var buf bytes.Buffer
	if err := mdParser.Convert([]byte(input), &buf); err != nil {
		return Escape(input)
	}
	return markdownPolicy.Sanitize(buf.String())
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
