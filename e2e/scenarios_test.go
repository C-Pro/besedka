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
