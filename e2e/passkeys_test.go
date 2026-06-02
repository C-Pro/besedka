//go:build e2e

package e2e

import (
	"github.com/playwright-community/playwright-go"
	"testing"
)

func TestE2EPasskeys(t *testing.T) {
	t.Parallel()
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	context := createBrowserContext(t, browser)
	defer func() { _ = context.Close() }()

	page, err := context.NewPage()
	if err != nil {
		t.Fatalf("could not create page: %v", err)
	}

	cdp, err := context.NewCDPSession(page)
	if err != nil {
		t.Fatalf("could not create CDP session: %v", err)
	}
	_, err = cdp.Send("WebAuthn.enable", nil)
	if err != nil {
		t.Fatalf("could not enable WebAuthn CDP: %v", err)
	}
	_, err = cdp.Send("WebAuthn.addVirtualAuthenticator", map[string]interface{}{
		"options": map[string]interface{}{
			"protocol":            "ctap2",
			"transport":           "internal",
			"hasResidentKey":      true,
			"hasUserVerification": true,
			"isUserVerified":      true,
			"automaticPresenceSimulation": true,
		},
	})
	if err != nil {
		t.Fatalf("could not add virtual authenticator: %v", err)
	}

	aliceSetupLink := server.CreateUser(t, "alice_passkey")
	registerUser(t, page, aliceSetupLink, "Alice Passkey", "password123")

	// Navigate to profile
	err = page.Locator("#desktop-profile-avatar").Click()
	if err != nil {
		t.Fatalf("failed to click avatar: %v", err)
	}
	err = page.Locator("#desktop-profile-btn").Click()
	if err != nil {
		t.Fatalf("failed to click profile button: %v", err)
	}
	err = page.Locator("#profile-modal-overlay").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	if err != nil {
		t.Fatalf("failed to wait for profile modal: %v", err)
	}

	// Add passkey
	err = page.Locator("#passkey-register-btn").Click()
	if err != nil {
		t.Fatalf("failed to click passkey register: %v", err)
	}

	err = page.Locator("#passkey-success").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	if err != nil {
		t.Fatalf("passkey registration success message not found: %v", err)
	}

	// Verify passkey list has an item
	count, err := page.Locator(".passkeys-list .passkey-item").Count()
	if err != nil || count != 1 {
		t.Fatalf("expected 1 passkey, got %d. error: %v", count, err)
	}

	// Log out
	err = page.Locator("#profile-modal-close").Click()
	if err != nil {
		t.Fatalf("failed to close profile modal: %v", err)
	}
	err = page.Locator("#desktop-profile-avatar").Click()
	if err != nil {
		t.Fatalf("failed to click avatar: %v", err)
	}
	err = page.Locator("#desktop-logoff-btn").Click()
	if err != nil {
		t.Fatalf("failed to click logoff: %v", err)
	}

	// Verify login screen
	err = page.Locator(".login-container").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	if err != nil {
		t.Fatalf("login modal not found: %v", err)
	}

	// Login with passkey
	err = page.Locator("#passkey-login-btn").Click()
	if err != nil {
		t.Fatalf("failed to click passkey login: %v", err)
	}

	// Verify logged in
	err = page.Locator(".app-layout").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	if err != nil {
		t.Fatalf("failed to login via passkey: %v", err)
	}

	// Delete passkey
	err = page.Locator("#desktop-profile-avatar").Click()
	if err != nil {
		t.Fatalf("failed to click avatar: %v", err)
	}
	err = page.Locator("#desktop-profile-btn").Click()
	if err != nil {
		t.Fatalf("failed to click profile btn: %v", err)
	}
	err = page.Locator("#profile-modal-overlay").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	if err != nil {
		t.Fatalf("profile modal not visible: %v", err)
	}

	page.OnDialog(func(dialog playwright.Dialog) {
		dialog.Accept()
	})

	err = page.Locator(".passkey-item button.btn-danger").Click()
	if err != nil {
		t.Fatalf("failed to click delete passkey: %v", err)
	}

	err = page.Locator(".passkey-item").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateHidden,
	})
	if err != nil {
		errorText, _ := page.Locator("#passkey-error").TextContent()
		t.Fatalf("passkey item did not disappear: %v, UI Error: %s", err, errorText)
	}

	count, err = page.Locator(".passkeys-list .passkey-item").Count()
	if err != nil || count != 0 {
		t.Fatalf("expected 0 passkeys after deletion, got %d", count)
	}
}
