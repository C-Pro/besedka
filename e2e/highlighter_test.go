//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestE2ECodeHighlighter(t *testing.T) {
	t.Parallel()
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	aliceLink := server.CreateUser(t, "alice")
	ctx := createBrowserContext(t, browser)
	page, err := ctx.NewPage()
	require.NoError(t, err)
	registerUser(t, page, aliceLink, "Alice Smith", "password123")

	selectTownhall(t, page)

	rawBlock := "// comment\nconst x = (42 + \"hi\");"
	msg := "Inline: `const y = 1;` and block:\n```js\n" + rawBlock + "\n```"

	require.NoError(t, page.Locator("#message-input").Fill(msg))
	require.NoError(t, page.Locator("#send-btn").Click())

	codeBlock := page.Locator(".messages-container .message-content pre code")
	require.NoError(t, codeBlock.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}))

	// Verify expected token spans exist inside <pre><code>
	require.Eventually(t, func() bool {
		return evalBool(t, page, `() => {
			const code = document.querySelector('.messages-container .message-content pre code');
			if (!code) return false;
			return Boolean(
				code.querySelector('.hl-comment') &&
				code.querySelector('.hl-keyword') &&
				code.querySelector('.hl-number') &&
				code.querySelector('.hl-string') &&
				code.querySelector('.hl-operator') &&
				code.querySelectorAll('.hl-bracket').length === 2
			);
		}`)
	}, 5*time.Second, 100*time.Millisecond)

	// Verify exact code text is preserved for copying
	textVal, err := codeBlock.Evaluate(`el => el.innerText`, nil)
	require.NoError(t, err)
	textStr, ok := textVal.(string)
	require.True(t, ok)
	require.Equal(t, rawBlock, strings.TrimRight(textStr, "\n"))

	// Verify inline <code> is NOT highlighted
	inlineHasHL := evalBool(t, page, `() => {
		const inlineCodes = Array.from(document.querySelectorAll('.messages-container .message-content code'))
			.filter(el => !el.closest('pre'));
		return inlineCodes.length === 1 && inlineCodes[0].querySelector('[class^="hl-"]') !== null;
	}`)
	require.False(t, inlineHasHL, "inline code must not be syntax highlighted")

	// Verify hovering one bracket activates both brackets in the pair
	firstBracket := codeBlock.Locator(".hl-bracket").First()
	require.NoError(t, firstBracket.Hover())
	require.Eventually(t, func() bool {
		return evalBool(t, page, `() => document.querySelectorAll('.messages-container .message-content pre code .hl-bracket-active').length === 2`)
	}, 3*time.Second, 50*time.Millisecond)

	// Verify dark and light themes apply distinct keyword colors
	darkColor, err := page.Evaluate(`() => getComputedStyle(document.querySelector('.messages-container .message-content pre code .hl-keyword')).color`)
	require.NoError(t, err)
	_, err = page.Evaluate(`() => { document.documentElement.dataset.theme = 'light'; }`)
	require.NoError(t, err)
	lightColor, err := page.Evaluate(`() => getComputedStyle(document.querySelector('.messages-container .message-content pre code .hl-keyword')).color`)
	require.NoError(t, err)
	require.NotEmpty(t, darkColor)
	require.NotEmpty(t, lightColor)
	require.NotEqual(t, darkColor, lightColor)
}
