//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

// selectTownhall opens the shared "townhall" channel. It dispatches the click
// rather than using Click() because on the mobile layout the sidebar is not
// "visible" to Playwright until a chat is selected.
func selectTownhall(t *testing.T, page playwright.Page) {
	item := page.Locator(".chat-item[data-id='townhall']")
	require.NoError(t, item.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateAttached,
	}))
	require.NoError(t, item.DispatchEvent("click", nil))
}

// waitTownhallMessage waits until the page's store has received a townhall
// message whose raw text contains substr (independent of which chat is open).
func waitTownhallMessage(t *testing.T, page playwright.Page, substr string) {
	require.Eventually(t, func() bool {
		v, err := page.Evaluate(
			`(s) => (window.store?.state?.messages?.townhall || []).some(m => (m.rawText || m.text || '').includes(s))`,
			substr,
		)
		if err != nil {
			return false
		}
		b, _ := v.(bool)
		return b
	}, 8*time.Second, 200*time.Millisecond, "townhall message %q not received", substr)
}

func evalBool(t *testing.T, page playwright.Page, expr string) bool {
	v, err := page.Evaluate(expr)
	require.NoError(t, err)
	b, _ := v.(bool)
	return b
}

// TestE2EMentionsAndSound covers the notification-sound trigger, the mention
// autocomplete (selection via click), and per-viewer mention rendering.
func TestE2EMentionsAndSound(t *testing.T) {
	t.Parallel()
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	// Register bob first so alice sees him in her user list on load.
	bobLink := server.CreateUser(t, "bob")
	aliceLink := server.CreateUser(t, "alice")

	bobCtx := createBrowserContext(t, browser)
	bobPage, err := bobCtx.NewPage()
	require.NoError(t, err)
	registerUser(t, bobPage, bobLink, "Bob Jones", "password456")

	aliceCtx := createBrowserContext(t, browser)
	alicePage, err := aliceCtx.NewPage()
	require.NoError(t, err)
	registerUser(t, alicePage, aliceLink, "Alice Smith", "password123")

	// Alice watches her DM with Bob; Bob posts in townhall.
	bobInAliceList := alicePage.Locator(".chat-item:has-text(\"Bob Jones\")")
	require.NoError(t, bobInAliceList.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}))
	require.NoError(t, bobInAliceList.Click())
	selectTownhall(t, bobPage)

	// --- Sound: a plain townhall message must NOT play a sound for Alice, who
	// is viewing a different chat (all-messages off, not a DM, no mention). ---
	require.NoError(t, bobPage.Locator("#message-input").Fill("plain townhall message"))
	require.NoError(t, bobPage.Locator("#send-btn").Click())
	waitTownhallMessage(t, alicePage, "plain townhall message")
	require.True(t, evalBool(t, alicePage, `() => window.store._soundPlays === 0`),
		"plain message should not trigger a sound while viewing another chat")

	// --- Sound: a message mentioning Alice MUST play a sound. ---
	require.NoError(t, bobPage.Locator("#message-input").Fill("@alice ping"))
	require.NoError(t, bobPage.Locator("#send-btn").Click())
	waitTownhallMessage(t, alicePage, "ping")
	require.Eventually(t, func() bool {
		return evalBool(t, alicePage, `() => window.store._soundPlays >= 1`)
	}, 8*time.Second, 200*time.Millisecond, "mention should trigger a sound")

	// --- Autocomplete: Alice switches to townhall and @-mentions Bob. ---
	selectTownhall(t, alicePage)
	aliceInput := alicePage.Locator("#message-input")
	require.NoError(t, aliceInput.Click())
	require.NoError(t, aliceInput.Fill("@b"))

	dropdown := alicePage.Locator(".mention-autocomplete")
	require.NoError(t, dropdown.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}))
	// Confirm via the pointerdown selection path.
	require.NoError(t, alicePage.Locator(".mention-item[data-username='bob']").DispatchEvent("pointerdown", nil))
	val, err := aliceInput.InputValue()
	require.NoError(t, err)
	require.Contains(t, val, "@bob", "autocomplete should insert @bob")

	// --- Render: send a message mentioning both bob and alice. ---
	require.NoError(t, aliceInput.Fill("@bob hi @alice "))
	require.NoError(t, alicePage.Locator("#send-btn").Click())

	// Bob's view: his own mention is highlighted as self; alice's is not.
	require.Eventually(t, func() bool {
		c, e := bobPage.Locator(".messages-container .mention-self:has-text(\"@bob\")").Count()
		return e == nil && c >= 1
	}, 8*time.Second, 200*time.Millisecond, "bob should see @bob as a self-mention")
	selfAliceForBob, err := bobPage.Locator(".messages-container .mention-self:has-text(\"@alice\")").Count()
	require.NoError(t, err)
	require.Equal(t, 0, selfAliceForBob, "bob should NOT see @alice as a self-mention")

	// Alice's view: her own mention is highlighted as self; bob's is not.
	require.Eventually(t, func() bool {
		c, e := alicePage.Locator(".messages-container .mention-self:has-text(\"@alice\")").Count()
		return e == nil && c >= 1
	}, 8*time.Second, 200*time.Millisecond, "alice should see @alice as a self-mention")
	selfBobForAlice, err := alicePage.Locator(".messages-container .mention-self:has-text(\"@bob\")").Count()
	require.NoError(t, err)
	require.Equal(t, 0, selfBobForAlice, "alice should NOT see @bob as a self-mention")
}

// TestE2EMentionAutocompleteMobile verifies the autocomplete works under a
// mobile viewport with touch: tapping a result inserts the mention and keeps
// the textarea focused (so the on-screen keyboard would stay up).
func TestE2EMentionAutocompleteMobile(t *testing.T) {
	t.Parallel()
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	// Bob must be registered (active) to appear in Alice's mention list.
	bobLink := server.CreateUser(t, "bob")
	aliceLink := server.CreateUser(t, "alice")

	bobCtx := createBrowserContext(t, browser)
	bobPage, err := bobCtx.NewPage()
	require.NoError(t, err)
	registerUser(t, bobPage, bobLink, "Bob Jones", "password456")

	mobileCtx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Permissions: []string{"notifications"},
		Viewport:    &playwright.Size{Width: 375, Height: 667},
		IsMobile:    playwright.Bool(true),
		HasTouch:    playwright.Bool(true),
	})
	require.NoError(t, err)
	mobileCtx.SetDefaultTimeout(5000)
	alicePage, err := mobileCtx.NewPage()
	require.NoError(t, err)
	registerUser(t, alicePage, aliceLink, "Alice Smith", "password123")

	selectTownhall(t, alicePage)
	input := alicePage.Locator("#message-input")
	require.NoError(t, input.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}))
	require.NoError(t, input.Click())
	require.NoError(t, input.Fill("@b"))

	dropdown := alicePage.Locator(".mention-autocomplete")
	require.NoError(t, dropdown.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}))

	// Tap the result (touch). The pointerdown+preventDefault keeps focus.
	require.NoError(t, alicePage.Locator(".mention-item[data-username='bob']").Tap())

	val, err := input.InputValue()
	require.NoError(t, err)
	require.Contains(t, val, "@bob", "tapping a result should insert the mention")
	require.True(t, evalBool(t, alicePage, `() => document.activeElement && document.activeElement.id === 'message-input'`),
		"the textarea should stay focused after a tap selection")
}

// TestE2ESettingsPersist verifies notification settings round-trip through the
// server: a toggle survives a full page reload (which re-fetches from the API).
func TestE2ESettingsPersist(t *testing.T) {
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

	openSettings := func() {
		require.NoError(t, page.Locator("#desktop-profile-avatar").Click())
		require.NoError(t, page.Locator("#desktop-settings-btn").Click())
		require.NoError(t, page.Locator("#settings-modal").WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		}))
	}

	openSettings()
	dmInput := page.Locator("#settings-modal input[data-setting='soundDirectMessages']")
	checked, err := dmInput.IsChecked()
	require.NoError(t, err)
	require.True(t, checked, "direct-message sound should be on by default")

	// Toggle it off via the visible slider (the input itself is size-zero).
	require.NoError(t, page.Locator("#settings-modal .settings-row:has(input[data-setting='soundDirectMessages']) .ios-toggle-slider").Click())
	// Wait for the optimistic store update, then give the POST time to persist.
	require.Eventually(t, func() bool {
		return evalBool(t, page, `() => window.store.settings.notifications.soundDirectMessages === false`)
	}, 5*time.Second, 100*time.Millisecond)
	time.Sleep(1 * time.Second)

	// Reload: settings are re-fetched from the server.
	_, err = page.Reload()
	require.NoError(t, err)
	require.NoError(t, page.Locator("#app").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	}))

	require.Eventually(t, func() bool {
		return evalBool(t, page, `() => window.store.settings.notifications.soundDirectMessages === false`)
	}, 5*time.Second, 200*time.Millisecond, "setting should persist server-side across reload")

	openSettings()
	checked, err = page.Locator("#settings-modal input[data-setting='soundDirectMessages']").IsChecked()
	require.NoError(t, err)
	require.False(t, checked, "reloaded settings dialog should reflect the persisted value")
}
