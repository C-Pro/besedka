//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestE2EMultiDeviceReadReceipts(t *testing.T) {
	t.Parallel()
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	// 1. Create setup links
	aliceSetupLink := server.CreateUser(t, "alice")
	bobSetupLink := server.CreateUser(t, "bob")

	// 2. Register Alice on Context 1 (Device A)
	t.Log("Registering Alice...")
	aliceContext1 := createBrowserContext(t, browser)
	alicePage1, err := aliceContext1.NewPage()
	require.NoError(t, err)
	aliceSecret := registerUser(t, alicePage1, aliceSetupLink, "Alice Smith", "password123")

	// 3. Log in Alice on Context 2 (Device B)
	t.Log("Logging in Alice on Device B...")
	aliceContext2 := createBrowserContext(t, browser)
	alicePage2, err := aliceContext2.NewPage()
	require.NoError(t, err)
	loginViaForm(t, alicePage2, server.BaseURL, "alice", "password123", aliceSecret)

	// 4. Register Bob on Context 3 (Bob's Device)
	t.Log("Registering Bob...")
	bobContext := createBrowserContext(t, browser)
	bobPage, err := bobContext.NewPage()
	require.NoError(t, err)
	registerUser(t, bobPage, bobSetupLink, "Bob Jones", "password456")

	// 5. Ensure all layouts are visible
	err = alicePage1.Locator(".app-layout").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	err = alicePage2.Locator(".app-layout").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	err = bobPage.Locator(".app-layout").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// 6. Bob selects Alice Smith and sends a DM message
	t.Log("Bob sends a DM message to Alice...")
	err = bobPage.Locator(".chat-item:has-text(\"Alice Smith\")").Click()
	require.NoError(t, err)

	err = bobPage.Locator("#message-input").Fill("Hello Alice from Bob!")
	require.NoError(t, err)
	err = bobPage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Locators for Bob Jones chat on Alice's Device A and B
	aliceBobChat1 := alicePage1.Locator(".chat-item:has-text(\"Bob Jones\")")
	aliceBobChat2 := alicePage2.Locator(".chat-item:has-text(\"Bob Jones\")")

	// 7. Verify both Device A and B show the unread badge "1" for Bob Jones
	t.Log("Verifying unread badges on Alice's devices...")
	require.Eventually(t, func() bool {
		badge1, err := aliceBobChat1.Locator(".unread-badge").InnerText()
		if err != nil {
			return false
		}
		badge2, err := aliceBobChat2.Locator(".unread-badge").InnerText()
		if err != nil {
			return false
		}
		return badge1 == "1" && badge2 == "1"
	}, 5*time.Second, 200*time.Millisecond, "Unread badge '1' should show on both Alice devices")

	// 8. Alice clicks Bob Jones chat on Device A (marks as read)
	t.Log("Alice reads the message on Device A...")
	err = aliceBobChat1.Click()
	require.NoError(t, err)

	// Verify the badge disappears on Device A
	require.Eventually(t, func() bool {
		count, _ := aliceBobChat1.Locator(".unread-badge").Count()
		return count == 0
	}, 5*time.Second, 200*time.Millisecond, "Unread badge should disappear on Device A")

	// 9. Verify the badge ALSO disappears on Device B (multi-device synchronization)
	t.Log("Verifying badge synchronization on Device B...")
	require.Eventually(t, func() bool {
		count, _ := aliceBobChat2.Locator(".unread-badge").Count()
		return count == 0
	}, 5*time.Second, 200*time.Millisecond, "Unread badge should disappear on Device B via WS sync")

	// 10. Alice sends a DM back to Bob from Device A
	// (Since Bob's chat is active on Device A, but remains closed/inactive on Device B)
	t.Log("Alice sends a message back to Bob from Device A...")
	err = alicePage1.Locator("#message-input").Fill("Hello Bob from Alice!")
	require.NoError(t, err)
	err = alicePage1.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Wait for Bob to receive it to confirm message was processed
	require.Eventually(t, func() bool {
		content, _ := bobPage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "Hello Bob from Alice!")
	}, 5*time.Second, 200*time.Millisecond)

	// 11. Verify Device B (Bob's chat closed) does NOT show an unread badge for Alice's own message
	t.Log("Verifying Alice's own message does not trigger unread badge on Device B...")
	time.Sleep(200 * time.Millisecond)
	count, err := aliceBobChat2.Locator(".unread-badge").Count()
	require.NoError(t, err)
	require.Equal(t, 0, count, "Own message should not trigger unread badge on sibling devices")
}
