//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestE2ENavigationBack(t *testing.T) {
	t.Parallel()
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

		err = page.AddInitScript(playwright.Script{Content: playwright.String(`(() => {
			if (window !== window.top) return;
			const pathsKey = '__e2eVisitedPaths';
			const paths = JSON.parse(sessionStorage.getItem(pathsKey) || '[]');
			paths.push(location.pathname);
			sessionStorage.setItem(pathsKey, JSON.stringify(paths));

			const nativeFetch = window.fetch.bind(window);
			window.fetch = async (...args) => {
				const input = args[0];
				const value = typeof input === 'string' ? input : input?.url;
				const pathname = new URL(value, location.href).pathname;
				if (pathname === '/api/me' && sessionStorage.getItem('__e2ePauseNextMe') === 'true') {
					sessionStorage.removeItem('__e2ePauseNextMe');
					window.__e2eSessionCheckPaused = true;
					await new Promise(resolve => { window.__e2eReleaseSessionCheck = resolve; });
				}
				return nativeFetch(...args);
			};
		})()`)})
		require.NoError(t, err)

		resetVisitedPaths := func() {
			t.Helper()
			_, err := page.Evaluate(`() => sessionStorage.setItem('__e2eVisitedPaths', '[]')`)
			require.NoError(t, err)
		}
		assertLoginNotVisited := func() {
			t.Helper()
			visited, err := page.Evaluate(`() => JSON.parse(sessionStorage.getItem('__e2eVisitedPaths') || '[]').includes('/login.html')`)
			require.NoError(t, err)
			require.False(t, visited.(bool), "transient session failure must not visit the login document")
		}

		failures := []struct {
			name   string
			inject func(playwright.Route) error
		}{
			{
				name: "aborted request",
				inject: func(route playwright.Route) error {
					return route.Abort("failed")
				},
			},
			{
				name: "service unavailable",
				inject: func(route playwright.Route) error {
					return route.Fulfill(playwright.RouteFulfillOptions{
						Status: playwright.Int(503),
						Body:   "temporarily unavailable",
					})
				},
			},
		}

		for _, failure := range failures {
			resetVisitedPaths()
			injected := make(chan error, 1)
			err = page.Route("**/api/me", func(route playwright.Route) {
				injected <- failure.inject(route)
			}, 1)
			require.NoError(t, err)

			_, err = page.Reload()
			require.NoError(t, err)
			select {
			case err := <-injected:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatalf("the %s was not injected", failure.name)
			}
			err = page.Locator(".app-layout").WaitFor(playwright.LocatorWaitForOptions{
				State:   playwright.WaitForSelectorStateVisible,
				Timeout: playwright.Float(5000),
			})
			require.NoError(t, err, "valid session should recover from %s", failure.name)
			assertLoginNotVisited()
		}

		// 3. Press Back
		t.Log("Pressing back button...")
		_, err = page.GoBack()
		require.NoError(t, err)

		// 4. We should NOT be on login.html
		url := page.URL()
		t.Logf("URL after back: %s", url)
		require.False(t, strings.Contains(url, "login.html"), "Should NOT be on login page after pressing back")

		// If a valid session ever lands on the login route, the login page should
		// recover to the chat without requiring a browser Back action.
		_, err = page.Goto(server.BaseURL + "/login.html")
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			return !strings.Contains(page.URL(), "login.html")
		}, 5*time.Second, 100*time.Millisecond)
		err = page.Locator(".app-layout").WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		})
		require.NoError(t, err)

		// Exercise a real 401 from /api/me: let the authenticated app document
		// load, pause its session request, remove the cookie, then release it.
		resetVisitedPaths()
		_, err = page.Evaluate(`() => sessionStorage.setItem('__e2ePauseNextMe', 'true')`)
		require.NoError(t, err)
		_, err = page.Reload()
		require.NoError(t, err)
		_, err = page.WaitForFunction(`() => window.__e2eSessionCheckPaused === true`, nil,
			playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(5000)})
		require.NoError(t, err)

		response, err := page.ExpectResponse("**/api/me", func() error {
			if err := context.ClearCookies(playwright.BrowserContextClearCookiesOptions{Name: "token"}); err != nil {
				return err
			}
			_, err := page.Evaluate(`() => window.__e2eReleaseSessionCheck()`)
			return err
		})
		require.NoError(t, err)
		require.Equal(t, 401, response.Status())
		err = page.Locator(".login-container").WaitFor(playwright.LocatorWaitForOptions{
			State:   playwright.WaitForSelectorStateVisible,
			Timeout: playwright.Float(5000),
		})
		require.NoError(t, err)
		require.Contains(t, page.URL(), "/login.html")
		paths, err := page.Evaluate(`() => sessionStorage.getItem('__e2eVisitedPaths')`)
		require.NoError(t, err)
		require.JSONEq(t, `["/", "/login.html"]`, paths.(string))
	})

	// Scenario 2: Mobile - Back within the app tabs
	t.Run("Mobile Tab Navigation Back", func(t *testing.T) {
		bobSetupLink := server.CreateUser(t, "bob_nav")

		// Create mobile context
		context, err := browser.NewContext(playwright.BrowserNewContextOptions{
			Permissions: []string{"notifications"},
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

		// Wait until either chat-area is visible OR sidebar is visible and populated
		require.Eventually(t, func() bool {
			windowVisible, _ := page.Locator("#chat-area").IsVisible()
			if windowVisible {
				return true
			}
			listVisible, _ := page.Locator(".chat-item:has-text(\"Town Hall\")").IsVisible()
			return listVisible
		}, 10*time.Second, 200*time.Millisecond)

		windowVisible, _ := page.Locator("#chat-area").IsVisible()
		if !windowVisible {
			t.Log("On chat-list, clicking Town Hall...")
			err = page.Locator(".chat-item:has-text(\"Town Hall\")").DispatchEvent("click", nil)
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
		err = page.Locator("#hamburger-btn").DispatchEvent("click", nil)
		require.NoError(t, err)

		t.Log("Clicking Info tab...")
		err = page.Locator(".mobile-menu-item[data-tab='info-panel']").DispatchEvent("click", nil)
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

		// 6.5 Click on the active chat (Town Hall) in the chat list. It should open the chat window even though it was already active.
		t.Log("On chat-list, clicking already active Town Hall chat...")
		err = page.Locator(".chat-item:has-text(\"Town Hall\")").DispatchEvent("click", nil)
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			visible, _ := page.Locator("#chat-area").IsVisible()
			return visible
		}, 5*time.Second, 100*time.Millisecond, "Should open chat window on clicking active chat")

		// Go back to the chat list to restore position for the final back test
		t.Log("Pressing back to return to Chat List...")
		_, err = page.GoBack()
		require.NoError(t, err)

		require.Eventually(t, func() bool {
			visible, _ := page.Locator("#sidebar").IsVisible()
			return visible
		}, 5*time.Second, 100*time.Millisecond, "Should be back on chat list again")

		// 7. Press Back again (should NOT be login page)
		t.Log("Pressing back (from Chat List)...")
		_, err = page.GoBack()
		require.NoError(t, err)

		url := page.URL()
		t.Logf("URL after back: %s", url)
		require.False(t, strings.Contains(url, "login.html"), "Should NOT be on login page")
	})
}
