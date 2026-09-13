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
		{"HTML tags", "Hello <b>World</b>", "Hello World"},
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

func TestFormatMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Plain text", "Hello", "<p>Hello</p>\n"},
		{"Bold and Italic", "**Bold** and *Italic*", "<p><strong>Bold</strong> and <em>Italic</em></p>\n"},
		{"Link", "[Open AI](https://openai.com)", "<p><a href=\"https://openai.com\" rel=\"noreferrer noopener\" target=\"_blank\">Open AI</a></p>\n"},
		{"Image (stripped)", "![Cute cat](cat.jpg)", "<p></p>\n"},
		{"Unsafe HTML", "Hello <script>alert(1)</script>", "<p>Hello alert(1)</p>\n"},
		{"Javascript Link", "[Click](javascript:alert(1))", "<p>Click</p>\n"},
		{"Multiple paragraphs", "One\n\nTwo", "<p>One</p>\n<p>Two</p>\n"},
		{"Raw URL", "https://example.com/test", "<p><a href=\"https://example.com/test\" rel=\"noreferrer noopener\" target=\"_blank\">https://example.com/test</a></p>\n"},
		{"Code with quotes", "`\"test\"`", "<p><code>&#34;test&#34;</code></p>\n"},
		{
			"Basic table",
			"| Header 1 | Header 2 |\n| --- | --- |\n| Cell 1 | Cell 2 |",
			"<table>\n<thead>\n<tr>\n<th>Header 1</th>\n<th>Header 2</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td>Cell 1</td>\n<td>Cell 2</td>\n</tr>\n</tbody>\n</table>\n",
		},
		{
			"Table with alignments",
			"| Left | Center | Right |\n| :--- | :---: | ---: |\n| 1 | 2 | 3 |",
			"<table>\n<thead>\n<tr>\n<th align=\"left\">Left</th>\n<th align=\"center\">Center</th>\n<th align=\"right\">Right</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td align=\"left\">1</td>\n<td align=\"center\">2</td>\n<td align=\"right\">3</td>\n</tr>\n</tbody>\n</table>\n",
		},
		{
			"Table with inline formatting and links",
			"| Feature | Status |\n| --- | --- |\n| **Bold** and *Italic* | `code` |\n| [Link](https://example.com) | @alice |",
			"<table>\n<thead>\n<tr>\n<th>Feature</th>\n<th>Status</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td><strong>Bold</strong> and <em>Italic</em></td>\n<td><code>code</code></td>\n</tr>\n<tr>\n<td><a href=\"https://example.com\" rel=\"noreferrer noopener\" target=\"_blank\">Link</a></td>\n<td>@alice</td>\n</tr>\n</tbody>\n</table>\n",
		},
		{
			"Table with XSS injection",
			"| Name | Payload |\n| --- | --- |\n| test | <script>alert(1)</script><a href=\"javascript:alert(2)\">bad</a> |",
			"<table>\n<thead>\n<tr>\n<th>Name</th>\n<th>Payload</th>\n</tr>\n</thead>\n<tbody>\n<tr>\n<td>test</td>\n<td>alert(1)bad</td>\n</tr>\n</tbody>\n</table>\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatMessage(tt.input); got != tt.expected {
				t.Errorf("FormatMessage() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractMentions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"No mentions", "Hello world!", nil},
		{"Single mention", "Hello @alice!", []string{"alice"}},
		{"Multiple mentions", "Hey @alice and @bob_123, look at @charlie", []string{"alice", "bob_123", "charlie"}},
		{"Duplicate mentions", "Ping @alice and @alice again", []string{"alice"}},
		{"Email address not mention", "Contact me at alice@example.com", nil},
		{"Trailing punctuation", "Hello @alice. How is @bob-? Also @carol_", []string{"alice", "bob", "carol"}},
		{"Internal punctuation preserved", "Hello @alice.smith and @bob_jones-jr", []string{"alice.smith", "bob_jones-jr"}},
		{"Email followed by mention", "Send to foo@bar.com or @admin", []string{"admin"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractMentions(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("ExtractMentions() = %v, want %v", got, tt.expected)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("ExtractMentions()[%d] = %v, want %v", i, got[i], tt.expected[i])
				}
			}
		})
	}
}
