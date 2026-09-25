//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestE2EThemePreference(t *testing.T) {
	t.Parallel()
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	aliceLink := server.CreateUser(t, "alice")
	ctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		ColorScheme: playwright.ColorSchemeLight,
	})
	require.NoError(t, err)
	ctx.SetDefaultTimeout(3000)
	page, err := ctx.NewPage()
	require.NoError(t, err)
	registerUser(t, page, aliceLink, "Alice Smith", "password123")

	// Dark remains the default even on a device that prefers light.
	require.Eventually(t, func() bool {
		return evalBool(t, page, `() => document.documentElement.dataset.theme === 'dark'
			&& window.store.settings.appearance.theme === 'dark'`)
	}, 5*time.Second, 100*time.Millisecond)

	openSettings := func() {
		require.NoError(t, page.Locator("#desktop-profile-avatar").Click())
		require.NoError(t, page.Locator("#desktop-settings-btn").Click())
		require.NoError(t, page.Locator("#settings-modal").WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		}))
	}
	selectTheme := func(theme string) {
		selector := "#settings-modal .theme-option:has(input[value='" + theme + "'])"
		require.NoError(t, page.Locator(selector).Click())
		require.Eventually(t, func() bool {
			disabled, err := page.Locator("#settings-modal input[name='theme']").First().IsDisabled()
			return err == nil && !disabled
		}, 5*time.Second, 100*time.Millisecond)
	}

	openSettings()
	selectTheme("light")
	require.True(t, evalBool(t, page, `() => document.documentElement.dataset.theme === 'light'
		&& document.documentElement.dataset.themePreference === 'light'
		&& getComputedStyle(document.documentElement).colorScheme === 'light'
		&& document.querySelector('meta[name="theme-color"]').content === '#f6f8fa'`))
	if screenshotDir := os.Getenv("BESEDKA_THEME_SCREENSHOT_DIR"); screenshotDir != "" {
		_, err = page.Screenshot(playwright.PageScreenshotOptions{
			Path: playwright.String(filepath.Join(screenshotDir, "theme-light-desktop.png")),
		})
		require.NoError(t, err)
	}

	// Prove the server value wins over a deliberately stale local startup cache.
	_, err = page.Evaluate(`localStorage.setItem('besedka.theme', 'dark')`)
	require.NoError(t, err)
	_, err = page.Reload()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return evalBool(t, page, `() => window.store.settings.appearance.theme === 'light'
			&& document.documentElement.dataset.theme === 'light'`)
	}, 5*time.Second, 100*time.Millisecond)

	openSettings()
	selectTheme("system")
	require.True(t, evalBool(t, page, `() => document.documentElement.dataset.theme === 'light'`))

	require.NoError(t, page.EmulateMedia(playwright.PageEmulateMediaOptions{
		ColorScheme: playwright.ColorSchemeDark,
	}))
	require.Eventually(t, func() bool {
		return evalBool(t, page, `() => document.documentElement.dataset.themePreference === 'system'
			&& document.documentElement.dataset.theme === 'dark'`)
	}, 3*time.Second, 50*time.Millisecond)

	if screenshotDir := os.Getenv("BESEDKA_THEME_SCREENSHOT_DIR"); screenshotDir != "" {
		_, err = page.Screenshot(playwright.PageScreenshotOptions{
			Path: playwright.String(filepath.Join(screenshotDir, "theme-dark-desktop.png")),
		})
		require.NoError(t, err)

		require.NoError(t, page.Locator("#settings-modal-close").Click())
		require.NoError(t, page.SetViewportSize(390, 844))
		require.NoError(t, page.Locator("#mobile-profile-avatar").Click())
		require.NoError(t, page.Locator("#mobile-settings-btn").Click())
		require.NoError(t, page.Locator("#settings-modal").WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		}))
		_, err = page.Screenshot(playwright.PageScreenshotOptions{
			Path: playwright.String(filepath.Join(screenshotDir, "theme-dark-mobile.png")),
		})
		require.NoError(t, err)
	}
}
