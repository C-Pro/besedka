//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestE2ENavigationBack(t *testing.T) {
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	// Scenario 1: Desktop - Back from home after login
	t.Run("Desktop Back after Login", func(t *testing.T) {
		aliceSetupLink := server.CreateUser(t, "alice_nav")
		context := createBrowserContext(t, browser)
		page, err := context.NewPage()
		require.NoError(t, err)

		// 1. Go to login page first (to have it in history)
		_, err = page.Goto(server.BaseURL + "/login.html")
		require.NoError(t, err)

		// 2. Register (which also logs in)
		registerUserWithReplace(t, page, aliceSetupLink, "Alice Nav", "password123", true)

		// We should be on the main page

		require.Contains(t, page.URL(), server.BaseURL+"/")

		// 3. Press Back
		t.Log("Pressing back button...")
		_, err = page.GoBack()
		require.NoError(t, err)

		// 4. We should NOT be on login.html
		url := page.URL()
		t.Logf("URL after back: %s", url)
		require.False(t, strings.Contains(url, "login.html"), "Should NOT be on login page after pressing back")
	})

	// Scenario 2: Mobile - Back within the app tabs
	t.Run("Mobile Tab Navigation Back", func(t *testing.T) {
		bobSetupLink := server.CreateUser(t, "bob_nav")
		
		// Create mobile context
		context, err := browser.NewContext(playwright.BrowserNewContextOptions{
			Viewport: &playwright.Size{
				Width:  375,
				Height: 667,
			},
			IsMobile:  playwright.Bool(true),
			HasTouch:  playwright.Bool(true),
			UserAgent: playwright.String("Mozilla/5.0 (iPhone; CPU iPhone OS 13_2_3 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.0.3 Mobile/15E148 Safari/004.1"),
		})
		require.NoError(t, err)
		defer context.Close()

		page, err := context.NewPage()
		require.NoError(t, err)

		// 1. Go to login page first (to have it in history)
		_, err = page.Goto(server.BaseURL + "/login.html")
		require.NoError(t, err)

		// 2. Register/Login
		t.Log("Registering Bob on mobile...")
		registerUserWithReplace(t, page, bobSetupLink, "Bob Nav", "password456", true)
		t.Log("Registered Bob on mobile.")

		// On mobile, the app might auto-select Town Hall and switch to chat-window tab
		t.Log("Waiting for either chat-list or auto-selected chat-window...")
		
		// Wait for either the chat item to be visible OR the chat area to be visible
		require.Eventually(t, func() bool {
			listVisible, _ := page.Locator(".chat-item:has-text(\"Town Hall\")").IsVisible()
			windowVisible, _ := page.Locator("#chat-area").IsVisible()
			return listVisible || windowVisible
		}, 10*time.Second, 200*time.Millisecond)

		listVisible, _ := page.Locator(".sidebar").IsVisible()
		if listVisible {
			t.Log("On chat-list, clicking Town Hall...")
			err = page.Locator(".chat-item:has-text(\"Town Hall\")").Click()
			require.NoError(t, err)
		} else {
			t.Log("Already on chat-window (auto-selected).")
		}

		// 3. Verify we are on chat-window
		t.Log("Verifying chat-window is visible...")
		require.Eventually(t, func() bool {
			visible, _ := page.Locator("#chat-area").IsVisible()
			return visible
		}, 5*time.Second, 100*time.Millisecond)

		// 4. Switch to Info tab via menu
		t.Log("Opening mobile menu...")
		err = page.Locator("#hamburger-btn").Click()
		require.NoError(t, err)
		
		t.Log("Clicking Info tab...")
		err = page.Locator(".mobile-menu-item[data-tab='info-panel']").Click()
		require.NoError(t, err)

		// Verify info panel is active
		t.Log("Verifying info-panel is visible...")
		require.Eventually(t, func() bool {
			visible, _ := page.Locator("#info-panel").IsVisible()
			return visible
		}, 5*time.Second, 100*time.Millisecond)

		// 5. Press Back (should go back to chat-window)
		t.Log("Pressing back (from Info to Chat Window)...")
		_, err = page.GoBack()
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			visible, _ := page.Locator("#chat-area").IsVisible()
			return visible
		}, 5*time.Second, 100*time.Millisecond, "Should be back on chat window")

		// 6. Press Back again (should go back to chat-list)
		t.Log("Pressing back (from Chat Window to Chat List)...")
		_, err = page.GoBack()
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			visible, _ := page.Locator("#sidebar").IsVisible()
			return visible
		}, 5*time.Second, 100*time.Millisecond, "Should be back on chat list")
		
		// 7. Press Back again (should NOT be login page)
		t.Log("Pressing back (from Chat List)...")
		_, err = page.GoBack()
		require.NoError(t, err)
		
		url := page.URL()
		t.Logf("URL after back: %s", url)
		require.False(t, strings.Contains(url, "login.html"), "Should NOT be on login page")
	})
}
