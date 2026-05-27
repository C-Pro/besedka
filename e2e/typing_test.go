//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestE2ETypingDisruption(t *testing.T) {
	t.Parallel()
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	aliceSetupLink := server.CreateUser(t, "alice")
	bobSetupLink := server.CreateUser(t, "bob")

	aliceContext := createBrowserContext(t, browser)
	alicePage, err := aliceContext.NewPage()
	require.NoError(t, err)
	registerUser(t, alicePage, aliceSetupLink, "Alice Smith", "password123")

	bobContext := createBrowserContext(t, browser)
	bobPage, err := bobContext.NewPage()
	require.NoError(t, err)
	registerUser(t, bobPage, bobSetupLink, "Bob Jones", "password456")

	// Reload Alice's page to make sure she sees Bob's updated display name
	_, err = alicePage.Reload()
	require.NoError(t, err)

	// Alice opens Bob's chat
	err = alicePage.Locator(".chat-item:has-text(\"Bob Jones\")").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)
	err = alicePage.Locator(".chat-item:has-text(\"Bob Jones\")").Click()
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".chat-header h3").InnerHTML()
		return strings.Contains(content, "Bob Jones")
	}, 5*time.Second, 200*time.Millisecond)

	// Alice starts typing
	draftMsg := "This is an unfinished message"
	err = alicePage.Locator("#message-input").Fill(draftMsg)
	require.NoError(t, err)
	err = alicePage.Locator("#message-input").Focus()
	require.NoError(t, err)
	
	// Move cursor to position 5
	_, err = alicePage.Locator("#message-input").Evaluate("el => el.setSelectionRange(5, 5)", nil)
	require.NoError(t, err)

	// Bob opens Alice's chat and sends a message
	err = bobPage.Locator("text=Alice Smith").Click()
	require.NoError(t, err)
	
	bobMsg := "Hello from Bob!"
	err = bobPage.Locator("#message-input").Fill(bobMsg)
	require.NoError(t, err)
	err = bobPage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Wait for Bob's message to appear in Alice's chat
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, bobMsg)
	}, 5*time.Second, 200*time.Millisecond)

	// Check if Alice's draft is still there
	inputValue, err := alicePage.Locator("#message-input").InputValue()
	require.NoError(t, err)
	require.Equal(t, draftMsg, inputValue, "Message input should preserve draft when a new message arrives")

	// Check if cursor position is preserved
	cursorPos, err := alicePage.Locator("#message-input").Evaluate("el => el.selectionStart", nil)
	require.NoError(t, err)
	require.Equal(t, 5, cursorPos.(int), "Cursor position should be preserved")

	// Check if input still has focus
	isFocused, err := alicePage.Locator("#message-input").Evaluate("el => document.activeElement === el", nil)
	require.NoError(t, err)
	require.True(t, isFocused.(bool), "Message input should retain focus when a new message arrives")
}
