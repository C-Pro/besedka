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
	t.Parallel()
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	users := []string{"alice", "charlie", "chad", "ivan", "bob"}
	displayNames := []string{"Alice", "Charlie", "Chad", "Ivan", "Bob Jones"}
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
		"That's awesome. I love the offline capabilities and push notifications. Here is how I register the service worker:\n```javascript\nnavigator.serviceWorker.register('/sw.js');\n```",
		"And the unread badges on the app icon are so useful! 🔔",
		"Agreed, great work on the PWA features everyone!",
	}

	userLats := []float64{
		51.5074,  // Alice (London)
		35.6762,  // Charlie (Tokyo)
		37.7749,  // Chad (San Francisco)
		55.7558,  // Ivan (Moscow)
		-33.8688, // Bob (Sydney)
	}
	userLngs := []float64{
		-0.1278,  // Alice (London)
		139.6503, // Charlie (Tokyo)
		-122.4194, // Chad (San Francisco)
		37.6173,  // Ivan (Moscow)
		151.2093, // Bob (Sydney)
	}

	secrets := make(map[string]string)

	// Phase 1: Create and register all users, upload avatars
	for i, u := range users {
		t.Logf("Registering user %s", u)
		setupLink := server.CreateUser(t, u)

		ctx := createBrowserContext(t, browser)
		err := ctx.GrantPermissions([]string{"geolocation"})
		require.NoError(t, err)
		err = ctx.SetGeolocation(&playwright.Geolocation{
			Latitude:  userLats[i],
			Longitude: userLngs[i],
		})
		require.NoError(t, err)

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

		err = ctx.Close()
		require.NoError(t, err)
	}

	// Phase 2: Send messages in Town Hall and DMs
	var bobCtx playwright.BrowserContext
	var bobPage playwright.Page

	for i, u := range users {
		t.Logf("User %s sending messages...", u)
		ctx := createBrowserContext(t, browser)
		err := ctx.GrantPermissions([]string{"geolocation"})
		require.NoError(t, err)
		err = ctx.SetGeolocation(&playwright.Geolocation{
			Latitude:  userLats[i],
			Longitude: userLngs[i],
		})
		require.NoError(t, err)

		page, err := ctx.NewPage()
		require.NoError(t, err)
		err = page.SetViewportSize(1920, 1080)
		require.NoError(t, err)

		loginViaForm(t, page, server.BaseURL, u, "password123", secrets[u])

		// Toggle location sharing on
		label := page.Locator(".ios-toggle")
		err = label.WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateVisible,
			Timeout: playwright.Float(10000),
		})
		require.NoError(t, err)

		toggle := page.Locator("#location-toggle")
		checked, err := toggle.IsChecked()
		require.NoError(t, err)
		if !checked {
			err = label.Click()
			require.NoError(t, err)
		}

		// Send message in Town Hall
		err = page.Locator(".chat-item:has-text(\"Town Hall\")").Click()
		require.NoError(t, err)
		time.Sleep(300 * time.Millisecond)

		if u == "ivan" {
			err = page.Locator("#file-input").SetInputFiles([]string{"../static/pelican-gemini-3.5.flash.svg"})
			require.NoError(t, err)
			err = page.Locator(".attach-indicator").WaitFor(playwright.LocatorWaitForOptions{
				State: playwright.WaitForSelectorStateVisible,
			})
			require.NoError(t, err)
		}

		err = page.Locator("#message-input").Fill(townhallMessages[i])
		require.NoError(t, err)
		err = page.Locator("#send-btn").Click()
		require.NoError(t, err)
		time.Sleep(300 * time.Millisecond)

		// Alice and Charlie send a DM to Bob for unread badges
		if u == "alice" || u == "charlie" {
			err = page.Locator(".chat-item:has-text(\"Bob Jones\")").Click()
			require.NoError(t, err)
			msg := fmt.Sprintf("Hey Bob! It's %s — you should try the new PWA install! 🚀", displayNames[i])
			err = page.Locator("#message-input").Fill(msg)
			require.NoError(t, err)
			err = page.Locator("#send-btn").Click()
			require.NoError(t, err)
			time.Sleep(300 * time.Millisecond)
		}

		if u == "bob" {
			bobCtx = ctx
			bobPage = page
		} else {
			err = ctx.Close()
			require.NoError(t, err)
		}
	}

	defer func() {
		if bobCtx != nil {
			_ = bobCtx.Close()
		}
	}()

	// Desktop screenshot: reuse Bob's session (no login needed)
	t.Log("Taking desktop screenshot as Bob...")
	err := bobPage.SetViewportSize(1920, 1080)
	require.NoError(t, err)

	// Click Town Hall
	err = bobPage.Locator(".chat-item:has-text(\"Town Hall\")").Click()
	require.NoError(t, err)
	time.Sleep(1 * time.Second)

	// Wait for the SVG attachment to be rendered
	err = bobPage.Locator(".message-attachment img").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Wait for all 5 location markers to appear on the map
	err = bobPage.Locator(".marker-container").First().WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		count, err := bobPage.Locator(".marker-container").Count()
		return err == nil && count == 5
	}, 5*time.Second, 200*time.Millisecond)
	time.Sleep(1 * time.Second)

	if os.Getenv("CI") == "" {
		_, err = bobPage.Screenshot(playwright.PageScreenshotOptions{
			Path: playwright.String("../static/screenshot_desktop.webp"),
		})
		require.NoError(t, err)
		t.Log("Desktop screenshot saved.")
	}

	// Mobile screenshot: reuse Bob's session via cookies
	t.Log("Taking mobile screenshot as Bob...")
	mobileCtx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport:          &playwright.Size{Width: 360, Height: 640},
		DeviceScaleFactor: playwright.Float(3.0),
		IsMobile:          playwright.Bool(true),
		HasTouch:          playwright.Bool(true),
		Geolocation: &playwright.Geolocation{
			Latitude:  userLats[4], // Bob
			Longitude: userLngs[4],
		},
		Permissions: []string{"geolocation"},
	})
	require.NoError(t, err)
	defer func() { _ = mobileCtx.Close() }()
	mobileCtx.SetDefaultTimeout(10000)

	// Copy cookies from bobCtx to mobileCtx
	cookies, err := bobCtx.Cookies()
	require.NoError(t, err)
	optionalCookies := make([]playwright.OptionalCookie, len(cookies))
	for idx, c := range cookies {
		optionalCookies[idx] = playwright.OptionalCookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   playwright.String(c.Domain),
			Path:     playwright.String(c.Path),
			Expires:  playwright.Float(c.Expires),
			HttpOnly: playwright.Bool(c.HttpOnly),
			Secure:   playwright.Bool(c.Secure),
			SameSite: c.SameSite,
		}
	}
	err = mobileCtx.AddCookies(optionalCookies)
	require.NoError(t, err)

	mobilePage, err := mobileCtx.NewPage()
	require.NoError(t, err)

	_, err = mobilePage.Goto(server.BaseURL)
	require.NoError(t, err)

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

	// Wait for the SVG attachment to be rendered
	err = mobilePage.Locator(".message-attachment img").WaitFor(playwright.LocatorWaitForOptions{
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
