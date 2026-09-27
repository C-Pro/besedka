//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestE2EFontScalePreference(t *testing.T) {
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
	secret := registerUser(t, page, aliceLink, "Alice Smith", "password123")

	require.Eventually(t, func() bool {
		return evalBool(t, page, `() => !("fontScale" in window.store.settings.appearance)
			&& document.documentElement.dataset.fontScale === '100'`)
	}, 5*time.Second, 100*time.Millisecond)

	openSettings := func() {
		require.NoError(t, page.Locator("#desktop-profile-avatar").Click())
		require.NoError(t, page.Locator("#desktop-settings-btn").Click())
		require.NoError(t, page.Locator("#settings-modal").WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		}))
	}

	openSettings()
	_, err = page.Evaluate(`() => {
		const slider = document.querySelector('#font-size-slider');
		slider.value = '150';
		slider.dispatchEvent(new Event('input', { bubbles: true }));
	}`)
	require.NoError(t, err)
	require.True(t, evalBool(t, page, `() => document.querySelector('#font-size-output').value === '150%'
		&& document.querySelector('#font-size-slider').getAttribute('aria-valuetext') === '150 percent'
		&& parseFloat(getComputedStyle(document.documentElement).fontSize) === 24
		&& localStorage.getItem('besedka.fontScale') === '150'
		&& !("fontScale" in window.store.settings.appearance)`), "input should persist only in this browser")

	// Exercise the largest supported scale with the light palette.
	settingsPayloads := make(chan string, 1)
	page.OnRequest(func(request playwright.Request) {
		if request.Method() != "POST" || !strings.HasSuffix(request.URL(), "/api/users/me/settings") {
			return
		}
		payload, payloadErr := request.PostData()
		if payloadErr != nil {
			return
		}
		select {
		case settingsPayloads <- payload:
		default:
		}
	})
	require.NoError(t, page.Locator("#settings-modal .theme-option:has(input[value='light'])").Click())
	require.Eventually(t, func() bool {
		disabled, disableErr := page.Locator("#settings-modal input[name='theme']").First().IsDisabled()
		return disableErr == nil && !disabled && evalBool(t, page, `() => window.store.settings.appearance.theme === 'light'
			&& document.documentElement.dataset.theme === 'light'
			&& document.querySelector("#settings-modal input[name='theme'][value='light']").checked`)
	}, 5*time.Second, 100*time.Millisecond)
	select {
	case payload := <-settingsPayloads:
		require.NotContains(t, payload, "fontScale", "server settings payload must exclude device-local font scale")
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for theme settings request")
	}
	require.True(t, evalBool(t, page, `() => document.querySelector('#settings-modal').scrollWidth
		<= document.querySelector('#settings-modal').clientWidth + 1`))
	require.NoError(t, page.SetViewportSize(769, 720))
	require.True(t, evalBool(t, page, `() => document.querySelector('.chat-area').getBoundingClientRect().width >= 280`),
		"large text should leave the central chat usable above the mobile breakpoint")

	if screenshotDir := os.Getenv("BESEDKA_THEME_SCREENSHOT_DIR"); screenshotDir != "" {
		_, err = page.Screenshot(playwright.PageScreenshotOptions{
			Path: playwright.String(filepath.Join(screenshotDir, "font-150-light-desktop.png")),
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
			Path: playwright.String(filepath.Join(screenshotDir, "font-150-light-mobile.png")),
		})
		require.NoError(t, err)
	}

	// Reloading this browser keeps its local scale even after server settings load.
	_, err = page.Reload()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return evalBool(t, page, `() => document.documentElement.dataset.fontScale === '150'
			&& localStorage.getItem('besedka.fontScale') === '150'
			&& !("fontScale" in window.store.settings.appearance)`)
	}, 5*time.Second, 100*time.Millisecond)

	// Tabs in the same browser profile follow local appearance changes immediately.
	sharedPage, err := ctx.NewPage()
	require.NoError(t, err)
	_, err = sharedPage.Goto(server.BaseURL)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return evalBool(t, sharedPage, `() => document.documentElement.dataset.fontScale === '150'`)
	}, 5*time.Second, 100*time.Millisecond)
	_, err = page.Evaluate(`() => window.besedkaAppearance.setFontScale(140)`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return evalBool(t, sharedPage, `() => document.documentElement.dataset.fontScale === '140'`)
	}, 5*time.Second, 100*time.Millisecond)
	_, err = page.Evaluate(`() => window.besedkaAppearance.setFontScale(150)`)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return evalBool(t, sharedPage, `() => document.documentElement.dataset.fontScale === '150'`)
	}, 5*time.Second, 100*time.Millisecond)
	require.NoError(t, sharedPage.Close())

	// A separate browser context for the same user keeps the default scale.
	freshContext := createBrowserContext(t, browser)
	defer func() { _ = freshContext.Close() }()
	freshPage, err := freshContext.NewPage()
	require.NoError(t, err)
	loginViaForm(t, freshPage, server.BaseURL, "alice", "password123", secret)
	require.Eventually(t, func() bool {
		return evalBool(t, freshPage, `() => document.documentElement.dataset.fontScale === '100'
			&& localStorage.getItem('besedka.fontScale') === null
			&& !("fontScale" in window.store.settings.appearance)`)
	}, 5*time.Second, 100*time.Millisecond)

	// Invalid local values fall back safely instead of leaking into the rendered scale.
	_, err = freshPage.Evaluate(`() => localStorage.setItem('besedka.fontScale', '999')`)
	require.NoError(t, err)
	_, err = freshPage.Reload()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return evalBool(t, freshPage, `() => document.documentElement.dataset.fontScale === '100'
			&& getComputedStyle(document.documentElement).fontSize === '16px'`)
	}, 5*time.Second, 100*time.Millisecond)

	// Native range keyboard controls update and persist the preference.
	require.NoError(t, page.SetViewportSize(1280, 720))
	openSettings()
	slider := page.Locator("#font-size-slider")
	require.NoError(t, slider.Focus())
	require.NoError(t, slider.Press("ArrowLeft"))
	require.Eventually(t, func() bool {
		return evalBool(t, page, `() => document.documentElement.dataset.fontScale === '140'
			&& localStorage.getItem('besedka.fontScale') === '140'
			&& document.querySelector('#font-size-output').value === '140%'`)
	}, 5*time.Second, 100*time.Millisecond)

	require.NoError(t, page.Locator("#font-size-reset").Click())
	require.Eventually(t, func() bool {
		return evalBool(t, page, `() => document.documentElement.dataset.fontScale === '100'
			&& localStorage.getItem('besedka.fontScale') === '100'
			&& document.querySelector('#font-size-reset').disabled`)
	}, 5*time.Second, 100*time.Millisecond)

	_, err = page.Evaluate(`() => {
		const slider = document.querySelector('#font-size-slider');
		slider.value = '80';
		slider.dispatchEvent(new Event('input', { bubbles: true }));
	}`)
	require.NoError(t, err)
	require.True(t, evalBool(t, page, `() => document.documentElement.dataset.fontScale === '80'
		&& parseFloat(getComputedStyle(document.documentElement).fontSize) === 12.8
		&& document.querySelector('#font-size-output').value === '80%'`))

	// Closing the modal keeps the device-local value; no server commit is needed.
	require.NoError(t, page.Locator("#settings-modal-close").Click())
	require.True(t, evalBool(t, page, `() => document.documentElement.dataset.fontScale === '80'
		&& localStorage.getItem('besedka.fontScale') === '80'`))
	_, err = page.Reload()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return evalBool(t, page, `() => document.documentElement.dataset.fontScale === '80'`)
	}, 5*time.Second, 100*time.Millisecond)
}
