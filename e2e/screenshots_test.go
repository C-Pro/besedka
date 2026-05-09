//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestScreenshots(t *testing.T) {
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	users := []string{"alice", "charlie", "chad", "ivan", "bob"}
	displayNames := []string{"Alice", "Charlie", "Chad", "Ivan", "Bob"}
	avatarFiles := []string{
		"alice.png",
		"charlie.png",
		"chad.png",
		"ivan.png",
		"bob.png",
	}
	townhallMessages := []string{
		"Hey everyone! I heard we are getting PWA support in Besedka? 🎉",
		"Yes! We can install it on phones like a native app 📱",
		"That's awesome. I love the offline capabilities and push notifications.",
		"And the unread badges on the app icon are so useful! 🔔",
		"Agreed, great work on the PWA features everyone!",
	}

	secrets := make(map[string]string)

	for i, u := range users {
		t.Logf("Registering user %s", u)
		setupLink := server.CreateUser(t, u)

		ctx := createBrowserContext(t, browser)
		page, err := ctx.NewPage()
		require.NoError(t, err)

		secret := registerUser(t, page, setupLink, displayNames[i], "password123")
		secrets[u] = secret

		// Upload per-user avatar from e2e/avas/
		err = page.Locator("#desktop-profile-avatar").Click()
		require.NoError(t, err)
		err = page.Locator("#desktop-profile-btn").Click()
		require.NoError(t, err)
		err = page.Locator("#profile-modal").WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		})
		require.NoError(t, err)

		avatarPath := filepath.Join("avas", avatarFiles[i])
		err = page.Locator("#avatar-upload-input").SetInputFiles([]string{avatarPath})
		require.NoError(t, err)
		_, err = page.Evaluate(`() => document.querySelector('#avatar-save-btn').click()`)
		require.NoError(t, err)
		err = page.Locator("#avatar-success").WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		})
		require.NoError(t, err)

		err = page.Locator("#profile-modal-close").Click()
		require.NoError(t, err)

		// Send message in Town Hall
		err = page.Locator(".chat-item:has-text(\"Town Hall\")").Click()
		require.NoError(t, err)
		time.Sleep(300 * time.Millisecond)
		err = page.Locator("#message-input").Fill(townhallMessages[i])
		require.NoError(t, err)
		err = page.Locator("#send-btn").Click()
		require.NoError(t, err)
		time.Sleep(300 * time.Millisecond)

		// Alice and Charlie send a DM to Bob for unread badges
		if u == "alice" || u == "charlie" {
			err = page.Locator(fmt.Sprintf(".chat-item:has-text(\"%s\")", "Bob")).Click()
			if err != nil {
				time.Sleep(500 * time.Millisecond)
				err = page.Locator(".chat-item:has-text(\"Bob\")").Click()
			}
			if err == nil {
				msg := fmt.Sprintf("Hey Bob! It's %s — you should try the new PWA install! 🚀", displayNames[i])
				err = page.Locator("#message-input").Fill(msg)
				require.NoError(t, err)
				err = page.Locator("#send-btn").Click()
				require.NoError(t, err)
				time.Sleep(300 * time.Millisecond)
			}
		}

		err = ctx.Close()
		require.NoError(t, err)
	}

	// Desktop screenshot: log in as Bob
	t.Log("Taking desktop screenshot as Bob...")
	desktopCtx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport: &playwright.Size{Width: 1920, Height: 1080},
	})
	require.NoError(t, err)
	defer func() { _ = desktopCtx.Close() }()
	desktopCtx.SetDefaultTimeout(10000)

	desktopPage, err := desktopCtx.NewPage()
	require.NoError(t, err)

	loginViaForm(t, desktopPage, server.BaseURL, "bob", "password123", secrets["bob"])

	// Click Town Hall
	err = desktopPage.Locator(".chat-item:has-text(\"Town Hall\")").Click()
	require.NoError(t, err)
	time.Sleep(1 * time.Second)

	if os.Getenv("CI") == "" {
		_, err = desktopPage.Screenshot(playwright.PageScreenshotOptions{
			Path: playwright.String("../static/screenshot_desktop.webp"),
		})
		require.NoError(t, err)
		t.Log("Desktop screenshot saved.")
	}

	// Wait for TOTP to rotate so the mobile login doesn't hit replay protection
	desktopCode := getTOTP(t, secrets["bob"])
	for getTOTP(t, secrets["bob"]) == desktopCode {
		time.Sleep(1 * time.Second)
	}

	// Mobile screenshot: log in as Bob
	t.Log("Taking mobile screenshot as Bob...")
	mobileCtx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport:          &playwright.Size{Width: 360, Height: 640},
		DeviceScaleFactor: playwright.Float(3.0),
		IsMobile:          playwright.Bool(true),
		HasTouch:          playwright.Bool(true),
	})
	require.NoError(t, err)
	defer func() { _ = mobileCtx.Close() }()
	mobileCtx.SetDefaultTimeout(10000)

	mobilePage, err := mobileCtx.NewPage()
	require.NoError(t, err)

	loginViaForm(t, mobilePage, server.BaseURL, "bob", "password123", secrets["bob"])

	// On mobile, the app starts on the chat-list tab.
	// Wait for the chat list to populate.
	time.Sleep(2 * time.Second)

	// Click Town Hall to switch to chat-window tab with messages.
	// On mobile the sidebar may not be visible yet; use DispatchEvent to bypass visibility checks.
	err = mobilePage.Locator(".chat-item[data-id='townhall']").DispatchEvent("click", nil)
	require.NoError(t, err)

	// Wait for the chat window to become active
	err = mobilePage.Locator("#chat-area").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)
	time.Sleep(1 * time.Second)

	if os.Getenv("CI") == "" {
		_, err = mobilePage.Screenshot(playwright.PageScreenshotOptions{
			Path: playwright.String("../static/screenshot_mobile.webp"),
		})
		require.NoError(t, err)
		t.Log("Mobile screenshot saved.")
	}
}

func loginViaForm(t *testing.T, page playwright.Page, baseURL, username, password, secret string) {
	t.Helper()
	_, err := page.Goto(baseURL + "/login.html")
	require.NoError(t, err)

	err = page.Locator("#username").Fill(username)
	require.NoError(t, err)
	err = page.Locator("#password").Fill(password)
	require.NoError(t, err)

	code := getTOTP(t, secret)
	err = page.Locator("#otp").Fill(code)
	require.NoError(t, err)
	err = page.Locator("#login-btn").Click()
	require.NoError(t, err)

	err = page.Locator("#app").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)
	time.Sleep(1 * time.Second)
}
