//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestE2EScrollPosition(t *testing.T) {
	t.Parallel()
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	aliceSetupLink := server.CreateUser(t, "alice")
	aliceContext := createBrowserContext(t, browser)
	alicePage, err := aliceContext.NewPage()
	require.NoError(t, err)

	registerUser(t, alicePage, aliceSetupLink, "Alice Smith", "password123")

	// Wait for default load of Town Hall
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".chat-header h3").InnerHTML()
		return strings.Contains(content, "Town Hall")
	}, 5*time.Second, 200*time.Millisecond)

	// Send 30 messages to fill the screen
	for i := 1; i <= 30; i++ {
		err = alicePage.Locator("#message-input").Fill(fmt.Sprintf("msg_%d", i))
		require.NoError(t, err)
		err = alicePage.Locator("#send-btn").Click()
		require.NoError(t, err)
	}

	// Wait until msg_30 is visible
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "msg_30")
	}, 5*time.Second, 200*time.Millisecond)

	// Rule 2: Keep at bottom as new messages arrive
	// Check if scrollTop is near scrollHeight
	isAtBottom := func() bool {
		res, err := alicePage.Evaluate(`() => {
			const c = document.querySelector('#messages-container');
			return (c.scrollHeight - c.scrollTop - c.clientHeight) < 50;
		}`)
		if err != nil {
			return false
		}
		v, ok := res.(bool)
		return ok && v
	}

	require.Eventually(t, isAtBottom, 5*time.Second, 100*time.Millisecond)

	// User scrolls back
	_, err = alicePage.Evaluate(`() => {
		const c = document.querySelector('#messages-container');
		c.scrollTop = 0;
		c.dispatchEvent(new Event('scroll'));
	}`)
	require.NoError(t, err)

	// Wait for scroll event to process
	time.Sleep(500 * time.Millisecond)

	// Another message arrives (we send it via Alice but Alice is scrolled up)
	// Actually if Alice sends it, the text input forces scroll to bottom in the app.
	// So let's create Bob to send the message.
	bobSetupLink := server.CreateUser(t, "bob")
	bobContext := createBrowserContext(t, browser)
	bobPage, err := bobContext.NewPage()
	require.NoError(t, err)
	registerUser(t, bobPage, bobSetupLink, "Bob Jones", "password123")

	require.Eventually(t, func() bool {
		content, _ := bobPage.Locator(".chat-header h3").InnerHTML()
		return strings.Contains(content, "Town Hall")
	}, 5*time.Second, 200*time.Millisecond)

	err = bobPage.Locator("#message-input").Fill("bob_msg_1")
	require.NoError(t, err)
	err = bobPage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Check Alice page received it
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "bob_msg_1")
	}, 5*time.Second, 200*time.Millisecond)

	// Rule 1: Do not override position if user scrolled back
	// Alice should still be near top
	res, err := alicePage.Evaluate(`() => {
		return document.querySelector('#messages-container').scrollTop < 500;
	}`)
	require.NoError(t, err)
	require.True(t, res.(bool), "Alice should remain scrolled up")

	// Rule 3: Navigate to chat page from somewhere else - navigate to last message
	// Bob sends DM to Alice so Alice has another chat to switch to
	err = bobPage.Locator(".chat-item").Filter(playwright.LocatorFilterOptions{HasText: "Alice Smith"}).Click()
	require.NoError(t, err)
	err = bobPage.Locator("#message-input").Fill("bob_dm_1")
	require.NoError(t, err)
	err = bobPage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Alice switches to Bob DM
	err = alicePage.Locator(".chat-item").Filter(playwright.LocatorFilterOptions{HasText: "Bob Jones"}).Click()
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".chat-header h3").InnerHTML()
		return strings.Contains(content, "Bob Jones")
	}, 5*time.Second, 200*time.Millisecond)

	require.Eventually(t, isAtBottom, 5*time.Second, 100*time.Millisecond, "Should be at bottom after switching to DM")

	// Alice switches BACK to Town Hall
	err = alicePage.Locator(".chat-item").Filter(playwright.LocatorFilterOptions{HasText: "Town Hall"}).Click()
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".chat-header h3").InnerHTML()
		return strings.Contains(content, "Town Hall")
	}, 5*time.Second, 200*time.Millisecond)

	// Should be at bottom for Town Hall too!
	require.Eventually(t, isAtBottom, 5*time.Second, 100*time.Millisecond, "Should be at bottom after navigating back to Town Hall")
}
