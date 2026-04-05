//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
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

func registerUser(t *testing.T, page playwright.Page, setupLink string, displayName string, password string) string {
	return registerUserWithReplace(t, page, setupLink, displayName, password, false)
}

func registerUserWithReplace(t *testing.T, page playwright.Page, setupLink string, displayName string, password string, replace bool) string {
	var err error
	if replace {
		_, err = page.Evaluate("window.location.replace('" + setupLink + "')")
	} else {
		_, err = page.Goto(setupLink)
	}
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
	return secret
}

func TestE2EProfileEdit(t *testing.T) {
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	aliceSetupLink := server.CreateUser(t, "alice")

	aliceContext := createBrowserContext(t, browser)
	alicePage, err := aliceContext.NewPage()
	require.NoError(t, err)

	alicePage.OnConsole(func(msg playwright.ConsoleMessage) {
		t.Logf("BROWSER CONSOLE: %s", msg.Text())
	})

	registerUser(t, alicePage, aliceSetupLink, "Alice Smith", "password123")

	err = alicePage.Locator(".app-layout").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Handle the dismissable dialogs for password reset confirm
	alicePage.OnDialog(func(dialog playwright.Dialog) {
		err := dialog.Accept()
		require.NoError(t, err)
	})

	// Open Profile Modal
	err = alicePage.Locator("#desktop-profile-avatar").Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(2000),
	})
	require.NoError(t, err, "click avatar")

	err = alicePage.Locator("#desktop-profile-btn").Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(2000),
	})
	require.NoError(t, err, "click profile btn")

	err = alicePage.Locator("#profile-modal").WaitFor(playwright.LocatorWaitForOptions{
		State:   playwright.WaitForSelectorStateVisible,
		Timeout: playwright.Float(2000),
	})
	require.NoError(t, err, "wait for modal")

	// 1. Change display name
	err = alicePage.Locator("#profile-display-name-input").Fill("Alice Wonderland")
	require.NoError(t, err)

	_, err = alicePage.Evaluate(`() => document.querySelector('#display-name-save-btn').click()`)
	require.NoError(t, err)

	_, err = alicePage.WaitForFunction(`() => {
		const els = document.querySelectorAll('#display-name-success');
		const el = els[els.length - 1]; // get the last one just in case
		console.log("WaitFor [display-name-success]: count=", els.length, "display=", el ? el.style.display : "null");
		return el && el.style.display !== 'none';
	}`, nil, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(5000)})
	require.NoError(t, err, "display name success message not shown")

	// 2. Upload Avatar
	imgPath := filepath.Join(t.TempDir(), "avatar.png")
	// 1x1 png file
	err = os.WriteFile(imgPath, []byte("\x89PNG\x0d\x0a\x1a\x0a\x00\x00\x00\x0dIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\x0aIDATx\x9cc\x00\x01\x00\x00\x05\x00\x01\x0d\x0a-\xb4\x00\x00\x00\x00IEND\xaeB`\x82"), 0644)
	require.NoError(t, err)

	err = alicePage.Locator("#avatar-upload-input").SetInputFiles([]string{imgPath})
	require.NoError(t, err)
	_, err = alicePage.Evaluate(`() => document.querySelector('#avatar-save-btn').click()`)
	require.NoError(t, err)

	_, err = alicePage.WaitForFunction(`() => {
		const el = document.querySelector('#avatar-success');
		return el && el.style.display !== 'none';
	}`, nil, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(5000)})
	require.NoError(t, err, "avatar success message not shown")

	// 3. Reset password
	_, err = alicePage.Evaluate(`() => document.querySelector('#password-reset-btn').click()`)
	require.NoError(t, err)

	_, err = alicePage.WaitForFunction(`() => {
		const el = document.querySelector('#password-reset-success');
		return el && el.style.display !== 'none';
	}`, nil, playwright.PageWaitForFunctionOptions{Timeout: playwright.Float(5000)})
	require.NoError(t, err, "password reset success message not shown")

	setupLinkText, err := alicePage.Locator("#password-reset-link").TextContent()
	require.NoError(t, err)
	require.Contains(t, setupLinkText, "/register.html?token=")

	// Click close modal
	err = alicePage.Locator("#profile-modal-close").Click()
	require.NoError(t, err)
}

func TestE2EReactivateDeletedUser(t *testing.T) {
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	// 1. Create a user
	aliceSetupLink := server.CreateUser(t, "alice")

	// 2. Register
	aliceContext := createBrowserContext(t, browser)
	alicePage, err := aliceContext.NewPage()
	require.NoError(t, err)
	registerUser(t, alicePage, aliceSetupLink, "Alice Smith", "password123")

	// 3. Admin deletes the user
	bobID := server.GetUserID(t, "alice")
	server.DeleteUser(t, bobID)

	// 4. Admin navigates to the Admin UI to reactivate
	adminContext := createBrowserContext(t, browser)
	adminContext.SetExtraHTTPHeaders(map[string]string{
		"Authorization": "Basic YWRtaW46MTMzN2NoYXQ=", // base64("admin:1337chat")
	})
	adminPage, err := adminContext.NewPage()
	require.NoError(t, err)

	adminPage.OnDialog(func(dialog playwright.Dialog) {
		err := dialog.Accept()
		require.NoError(t, err)
	})

	_, err = adminPage.Goto("http://" + server.AdminAddr)
	require.NoError(t, err)

	// User should be visible as deleted
	err = adminPage.Locator("tr.deleted:has-text(\"alice\")").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Click the Reactivate / Reset Password button
	err = adminPage.Locator("tr.deleted:has-text(\"alice\") >> button:has-text(\"Reactivate\")").Click()
	require.NoError(t, err)

	// After clicking, the UI should show the new registration link
	err = adminPage.Locator("#reg-link").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	newSetupLink, err := adminPage.Locator("#reg-link").InnerText()
	require.NoError(t, err)

	// 5. Register again using the new setup link
	registerUser(t, alicePage, newSetupLink, "Alice Reactivated", "newpassword123")

	// 6. Verify they can log in and see chat
	err = alicePage.Locator(".chat-item:has-text(\"Town Hall\")").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err, "Reactivated user should see Town Hall chat")
}

func TestE2EInfiniteScroll(t *testing.T) {
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	aliceSetupLink := server.CreateUser(t, "alice")
	aliceContext := createBrowserContext(t, browser)
	alicePage, err := aliceContext.NewPage()
	require.NoError(t, err)

	registerUser(t, alicePage, aliceSetupLink, "Alice Smith", "password123")

	// Wait for default load of Town Hall
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".chat-header h3").InnerHTML()
		return strings.Contains(content, "Town Hall")
	}, 5*time.Second, 200*time.Millisecond)

	// Send 110 messages
	t.Log("Sending 110 messages to fill the chat...")
	for i := 1; i <= 110; i++ {
		if i%10 == 0 {
			t.Logf("Sending message %d...", i)
		}
		err = alicePage.Locator("#message-input").Fill(fmt.Sprintf("fetch_scroll_test_msg_%d", i))
		require.NoError(t, err)
		err = alicePage.Locator("#send-btn").Click()
		require.NoError(t, err)
	}

	// Verify the latest message is loaded
	t.Log("Waiting for message 110...")
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "fetch_scroll_test_msg_110")
	}, 10*time.Second, 200*time.Millisecond)

	// Refresh the page to reload the chat from scratch (should fetch only the last 100 msgs initially)
	t.Log("Reloading page...")
	_, err = alicePage.Reload()
	require.NoError(t, err)
	
	// Wait for chat load
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "fetch_scroll_test_msg_110")
	}, 10*time.Second, 200*time.Millisecond)

	time.Sleep(1 * time.Second)

	// Check if msg 1 is NOT visible (since only the last 100 are loaded by default)
	content, _ := alicePage.Locator(".messages-container").InnerHTML()
	require.NotContains(t, content, "fetch_scroll_test_msg_1<", "msg 1 should not be loaded yet")
	
	// Scroll to top — set scrollTop and dispatch a scroll event since programmatic
	// changes to scrollTop don't fire scroll events in headless browsers.
	t.Log("Scrolling to top to trigger infinite scroll...")
	_, err = alicePage.Evaluate(`() => {
		const el = document.querySelector('#messages-container');
		el.scrollTop = 0;
		el.dispatchEvent(new Event('scroll'));
	}`)
	require.NoError(t, err)

	// Verify old messages appear
	t.Log("Waiting for msg 1...")
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "fetch_scroll_test_msg_1<")
	}, 10*time.Second, 200*time.Millisecond)
}

func TestE2EFileUpload(t *testing.T) {
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	aliceSetupLink := server.CreateUser(t, "alice")
	aliceContext := createBrowserContext(t, browser)
	alicePage, err := aliceContext.NewPage()
	require.NoError(t, err)

	registerUser(t, alicePage, aliceSetupLink, "Alice Smith", "password123")

	err = alicePage.Locator(".app-layout").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Create a dummy text file
	filePath := filepath.Join(t.TempDir(), "test_document.txt")
	err = os.WriteFile(filePath, []byte("Hello, this is a test document."), 0644)
	require.NoError(t, err)

	// Open Town Hall chat
	err = alicePage.Locator(".chat-item:has-text(\"Town Hall\")").Click()
	require.NoError(t, err)

	// Attach file
	err = alicePage.Locator("#file-input").SetInputFiles([]string{filePath})
	require.NoError(t, err)

	// Wait for attach indicator
	err = alicePage.Locator(".attach-indicator").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Send message
	err = alicePage.Locator("#message-input").Fill("Here is my file!")
	require.NoError(t, err)
	err = alicePage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Verify attachment appears in chat
	attachmentEl := alicePage.Locator(".message-attachment-file[data-name='test_document.txt']")
	err = attachmentEl.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	fileID, err := attachmentEl.GetAttribute("data-file-id")
	require.NoError(t, err)

	// Click to open download menu
	err = attachmentEl.Click()
	require.NoError(t, err)

	// Wait for menu
	err = alicePage.Locator("#file-download-menu a").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Download file
	download, err := alicePage.ExpectDownload(func() error {
		return alicePage.Locator("#file-download-menu a").Click()
	})
	require.NoError(t, err)

	require.Equal(t, fileID, download.SuggestedFilename())
}
