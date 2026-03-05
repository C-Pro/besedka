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

func TestE2ERestartRecovery(t *testing.T) {
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	// 1. Create users via API
	t.Log("Creating users via API...")
	aliceSetupLink := server.CreateUserAPI(t, "alice")
	bobSetupLink := server.CreateUserAPI(t, "bob")
	carolSetupLink := server.CreateUserAPI(t, "carol")

	// 2. Register Alice and stay logged in
	t.Log("Registering Alice...")
	aliceContext := createBrowserContext(t, browser)
	alicePage, err := aliceContext.NewPage()
	require.NoError(t, err)
	registerUser(t, alicePage, aliceSetupLink, "Alice Smith", "password123")

	// 3. Register Bob and log off
	t.Log("Registering Bob...")
	bobContext := createBrowserContext(t, browser)
	bobPage, err := bobContext.NewPage()
	require.NoError(t, err)

	bobSecret := registerUser(t, bobPage, bobSetupLink, "Bob Jones", "password456")

	// Alice reload to make sure she sees Bob
	_, err = alicePage.Reload()
	require.NoError(t, err)
	err = alicePage.Locator(".app-layout").WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible})
	require.NoError(t, err)

	t.Log("Logging Bob out...")
	err = bobPage.Locator("#desktop-profile-avatar").Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(5000),
	})
	require.NoError(t, err)
	err = bobPage.Locator("#desktop-logoff-btn").WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible})
	require.NoError(t, err)
	err = bobPage.Locator("#desktop-logoff-btn").Click()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return strings.Contains(bobPage.URL(), "login.html")
	}, 5*time.Second, 200*time.Millisecond)

	// Alice sends message to Town Hall
	t.Log("Alice sends message to Town Hall...")
	err = alicePage.Locator(".chat-item:has-text(\"Town Hall\")").Click()
	require.NoError(t, err)
	err = alicePage.Locator("#message-input").Fill("Hello before restart")
	require.NoError(t, err)
	err = alicePage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Alice sends DM to Bob
	t.Log("Alice sends DM to Bob...")
	err = alicePage.Locator(fmt.Sprintf(".chat-item:has-text(%q)", "Bob Jones")).Click()
	require.NoError(t, err)
	// wait for chat window to load
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".chat-header h3").InnerHTML()
		return strings.Contains(content, "Bob Jones")
	}, 5*time.Second, 200*time.Millisecond)
	err = alicePage.Locator("#message-input").Fill("DM before restart")
	require.NoError(t, err)
	err = alicePage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// 4. Restart Server
	t.Log("Restarting server...")
	server.Restart(t)
	t.Log("Server restarted.")

	// 5. Post-restart Verification

	// A. Alice should still be logged in and see her messages
	t.Log("Verifying Alice state...")
	_, err = alicePage.Reload()
	require.NoError(t, err)
	err = alicePage.Locator(".app-layout").WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible})
	require.NoError(t, err, "Alice should be still logged in")

	// Check Alice sees "Town Hall" message
	err = alicePage.Locator(".chat-item:has-text(\"Town Hall\")").Click()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "Hello before restart")
	}, 5*time.Second, 200*time.Millisecond, "Alice missing Town Hall message")

	// Check Alice sees "Bob Jones" DM
	err = alicePage.Locator(fmt.Sprintf(".chat-item:has-text(%q)", "Bob Jones")).Click()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "DM before restart")
	}, 5*time.Second, 200*time.Millisecond, "Alice missing DM message")

	// B. Carol registers using recovered setup link
	t.Log("Registering Carol...")
	carolContext := createBrowserContext(t, browser)
	carolPage, err := carolContext.NewPage()
	require.NoError(t, err)
	carolSecret := registerUser(t, carolPage, carolSetupLink, "Carol White", "password789")
	require.NotEmpty(t, carolSecret, "Carol should have a secret")

	err = carolPage.Locator(".app-layout").WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible})
	require.NoError(t, err, "Carol should be logged in")

	// C. Bob logs in using recovered TOTP
	t.Log("Logging Bob in...")
	_, err = bobPage.Goto(server.BaseURL + "/login.html")
	require.NoError(t, err)
	err = bobPage.Locator("#username").Fill("bob")
	require.NoError(t, err)
	err = bobPage.Locator("#password").Fill("password456")
	require.NoError(t, err)

	bobCode := getTOTP(t, bobSecret)
	err = bobPage.Locator("#otp").Fill(bobCode)
	require.NoError(t, err)
	err = bobPage.Locator("button[type='submit']").Click()
	require.NoError(t, err)

	err = bobPage.Locator(".app-layout").WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible})
	require.NoError(t, err, "Bob should be able to log in")

	// Verify Bob sees DM from Alice
	err = bobPage.Locator(fmt.Sprintf(".chat-item:has-text(%q)", "Alice Smith")).Click()
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		content, _ := bobPage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "DM before restart")
	}, 5*time.Second, 200*time.Millisecond, "Bob missing DM message")
}
