package content

import (
	"testing"
)

func TestSanitize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Plain text", "Hello World", "Hello World"},
		{"HTML tags", "Hello <b>World</b>", "Hello <b>World</b>"},
		{"Script tag", "<script>alert('xss')</script>Hello", "Hello"},
		{"Complex HTML", "<a href='javascript:alert(1)'>Click me</a>", "Click me"},
		{"Emoji", "I am 🤖", "I am 🤖"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Sanitize(tt.input); got != tt.expected {
				t.Errorf("Sanitize() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestEscape(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Plain text", "Hello World", "Hello World"},
		{"HTML chars", "<div>Hello</div>", "&lt;div&gt;Hello&lt;/div&gt;"},
		{"Quotes", `"Hello" 'World'`, "&#34;Hello&#34; &#39;World&#39;"},
		{"Emoji", "I am 🤖", "I am 🤖"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Escape(tt.input); got != tt.expected {
				t.Errorf("Escape() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"Valid alphanumeric", "user123", false},
		{"Valid with dot", "user.name", false},
		{"Valid with dash", "user-name", false},
		{"Valid with underscore", "user_name", false},
		{"Invalid space", "user name", true},
		{"Invalid special char", "user@name", true},
		{"Invalid script", "<script>", true},
		{"Empty", "", true},
		{"Mixed case", "User.Name-123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateUsername(tt.input); (err != nil) != tt.wantErr {
				t.Errorf("ValidateUsername() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
