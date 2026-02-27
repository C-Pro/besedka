//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestE2EMainFlow(t *testing.T) {
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	// 1. Create users via CLI first
	t.Log("Creating users via CLI...")
	aliceSetupLink := server.CreateUser(t, "alice")
	bobSetupLink := server.CreateUser(t, "bob")

	// 2. Register Alice
	t.Log("Registering Alice...")
	aliceContext := createBrowserContext(t, browser)
	alicePage, err := aliceContext.NewPage()
	require.NoError(t, err)
	registerUser(t, alicePage, aliceSetupLink, "Alice Smith", "password123")

	// 3. Register Bob
	t.Log("Registering Bob...")
	bobContext := createBrowserContext(t, browser)
	bobPage, err := bobContext.NewPage()
	require.NoError(t, err)
	registerUser(t, bobPage, bobSetupLink, "Bob Jones", "password456")

	// 4. Messaging Flow
	t.Log("Starting messaging flow...")

	// Reload Alice's page to make sure she sees Bob's updated display name
	_, err = alicePage.Reload()
	require.NoError(t, err)

	t.Log("Alice selects Bob...")

	err = alicePage.Locator(".chat-item:has-text(\"Bob Jones\")").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Try clicking by text
	err = alicePage.Locator(".chat-item:has-text(\"Bob Jones\")").Click()
	require.NoError(t, err)

	// Wait for chat window to load
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".chat-header h3").InnerHTML()
		return strings.Contains(content, "Bob Jones")
	}, 5*time.Second, 200*time.Millisecond)

	aliceMsg := "Hello Bob, how are you?"
	err = alicePage.Locator("#message-input").Fill(aliceMsg)
	require.NoError(t, err)
	err = alicePage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Bob receives message
	err = bobPage.Locator("text=Alice Smith").Click()
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		content, _ := bobPage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, aliceMsg)
	}, 5*time.Second, 200*time.Millisecond)

	// Bob replies
	bobReply := "Hi Alice! I am doing great."
	err = bobPage.Locator("#message-input").Fill(bobReply)
	require.NoError(t, err)
	err = bobPage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Alice receives reply
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, bobReply)
	}, 5*time.Second, 200*time.Millisecond)
}

func TestE2EDeleteUserRemovesChat(t *testing.T) {
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	// Create two users
	aliceSetupLink := server.CreateUser(t, "alice")
	bobSetupLink := server.CreateUser(t, "bob")

	// Register both
	aliceContext := createBrowserContext(t, browser)
	alicePage, err := aliceContext.NewPage()
	require.NoError(t, err)
	registerUser(t, alicePage, aliceSetupLink, "Alice Smith", "password123")

	bobContext := createBrowserContext(t, browser)
	bobPage, err := bobContext.NewPage()
	require.NoError(t, err)
	registerUser(t, bobPage, bobSetupLink, "Bob Jones", "password456")

	// Reload Alice to pick up Bob's display name
	_, err = alicePage.Reload()
	require.NoError(t, err)

	// Alice should see Bob's DM chat
	err = alicePage.Locator(".chat-item:has-text(\"Bob Jones\")").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err, "Alice should see Bob's DM chat before deletion")

	// Delete Bob via admin API
	bobID := server.GetUserID(t, "bob")
	server.DeleteUser(t, bobID)

	// Alice's chat list should update via WebSocket — Bob's DM should disappear
	require.Eventually(t, func() bool {
		count, _ := alicePage.Locator(".chat-item:has-text(\"Bob Jones\")").Count()
		return count == 0
	}, 5*time.Second, 200*time.Millisecond, "Bob's DM should disappear from Alice's chat list after deletion")

	// After page reload, Bob's DM should still be gone
	_, err = alicePage.Reload()
	require.NoError(t, err)

	// Wait for chat list to render
	err = alicePage.Locator(".chat-item:has-text(\"Town Hall\")").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	count, err := alicePage.Locator(".chat-item:has-text(\"Bob Jones\")").Count()
	require.NoError(t, err)
	require.Equal(t, 0, count, "Bob's DM should not appear after page reload")

	_ = bobPage // Bob's page exists but won't be usable after deletion
}

func TestE2ELogoff(t *testing.T) {
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	// Create user
	aliceSetupLink := server.CreateUser(t, "alice")

	// Register
	aliceContext := createBrowserContext(t, browser)
	alicePage, err := aliceContext.NewPage()
	require.NoError(t, err)
	registerUser(t, alicePage, aliceSetupLink, "Alice Smith", "password123")

	// Wait for the app layout to load
	t.Log("Waiting for app layout...")
	err = alicePage.Locator(".app-layout").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)
	t.Log("App layout visible.")

	// Since we run in desktop mode by default in these tests (viewport 1280x720 in createBrowserContext),
	// we interact with the desktop profile icon
	t.Log("Looking for desktop profile avatar...")
	err = alicePage.Locator("#desktop-profile-avatar").WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(5000),
	})
	require.NoError(t, err, "could not find #desktop-profile-avatar")
	t.Log("Clicking profile avatar...")

	err = alicePage.Locator("#desktop-profile-avatar").Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(5000),
	})
	require.NoError(t, err)
	t.Log("Clicked profile avatar.")

	// The dropdown should appear
	t.Log("Waiting for profile dropdown...")
	err = alicePage.Locator("#desktop-profile-dropdown").WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(5000),
	})
	require.NoError(t, err)
	t.Log("Profile dropdown visible.")

	// Click "Log Off"
	t.Log("Clicking log off button...")
	err = alicePage.Locator("#desktop-logoff-btn").Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(5000),
	})
	require.NoError(t, err)
	t.Log("Clicked log off button.")

	// Should be redirected to /login.html
	require.Eventually(t, func() bool {
		url := alicePage.URL()
		return strings.Contains(url, "login.html")
	}, 5*time.Second, 200*time.Millisecond, "User should be redirected to login page after log off")

	// Check that token is cleared by trying to navigate back to /
	_, err = alicePage.Goto(server.BaseURL + "/")
	require.NoError(t, err)

	// Should immediately redirect back to login
	require.Eventually(t, func() bool {
		url := alicePage.URL()
		return strings.Contains(url, "login.html")
	}, 5*time.Second, 200*time.Millisecond, "User should be redirected back to login page if token is cleared")
}

func registerUser(t *testing.T, page playwright.Page, setupLink string, displayName string, password string) {
	_, err := page.Goto(setupLink)
	require.NoError(t, err)

	// Wait for form to appear
	err = page.Locator("#register-form").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Extract TOTP secret from the page
	secretText, err := page.Locator("#totp-secret").InnerText()
	require.NoError(t, err)
	// format is "Secret: ABCDEFGHIJKLMNOP"
	secret := strings.TrimPrefix(secretText, "Secret: ")

	// Fill form
	err = page.Locator("#displayName").Fill(displayName)
	require.NoError(t, err)
	err = page.Locator("#password").Fill(password)
	require.NoError(t, err)

	// Generate TOTP
	code := getTOTP(t, secret)
	err = page.Locator("#totp").Fill(code)
	require.NoError(t, err)

	// Submit
	err = page.Locator("button[type='submit']").Click()
	require.NoError(t, err)

	// Should be redirected to / or index.html and see the app
	require.Eventually(t, func() bool {
		url := page.URL()
		return !strings.Contains(url, "register.html")
	}, 5*time.Second, 200*time.Millisecond)

	// Check if we are on the main page (contains #app)
	err = page.Locator("#app").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)
}
