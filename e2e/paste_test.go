//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestE2EPasteText(t *testing.T) {
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

	// Open Town Hall chat
	err = alicePage.Locator(".chat-item:has-text(\"Town Hall\")").Click()
	require.NoError(t, err)

	// Grant clipboard permissions to the browser context
	err = aliceContext.GrantPermissions([]string{"clipboard-read", "clipboard-write"})
	require.NoError(t, err)

	textarea := alicePage.Locator("#message-input")
	err = textarea.Focus()
	require.NoError(t, err)

	// Write text to clipboard via evaluate
	_, err = alicePage.Evaluate(`navigator.clipboard.writeText("Hello clipboard text!")`)
	require.NoError(t, err)

	// Type initial text in textarea
	err = textarea.Fill("Start End")
	require.NoError(t, err)

	// Move cursor to selection index 6 (between "Start" and "End")
	_, err = textarea.Evaluate("el => el.setSelectionRange(6, 6)", nil)
	require.NoError(t, err)

	// Trigger paste event via Control+V keypress
	err = alicePage.Keyboard().Press("Control+V")
	require.NoError(t, err)

	// Wait for the value to update to the pasted text inserted at the cursor position
	var val interface{}
	require.Eventually(t, func() bool {
		val, err = textarea.InputValue()
		if err != nil {
			return false
		}
		return val.(string) == "Start Hello clipboard text!End"
	}, 3*time.Second, 100*time.Millisecond)

	// Send message
	err = alicePage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Verify message in chat history
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "Start Hello clipboard text!End")
	}, 3*time.Second, 100*time.Millisecond)
}

func TestE2EPasteImage(t *testing.T) {
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

	// Open Town Hall chat
	err = alicePage.Locator(".chat-item:has-text(\"Town Hall\")").Click()
	require.NoError(t, err)

	// Programmatically dispatch paste event with image file
	pasteJs := `() => {
		const textarea = document.getElementById('message-input');
		const pngBase64 = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII=';
		const binaryString = atob(pngBase64);
		const len = binaryString.length;
		const bytes = new Uint8Array(len);
		for (let i = 0; i < len; i++) {
			bytes[i] = binaryString.charCodeAt(i);
		}
		const blob = new Blob([bytes], { type: 'image/png' });
		const file = new File([blob], 'clipboard_image.png', { type: 'image/png' });
		const dt = new DataTransfer();
		dt.items.add(file);
		const ev = new ClipboardEvent('paste', {
			clipboardData: dt,
			bubbles: true,
			cancelable: true
		});
		textarea.dispatchEvent(ev);
	}`

	_, err = alicePage.Evaluate(pasteJs)
	require.NoError(t, err)

	// Verify attachment indicator appears
	err = alicePage.Locator(".attach-indicator").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Send message
	err = alicePage.Locator("#message-input").Fill("Check out my pasted image!")
	require.NoError(t, err)
	err = alicePage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Verify the image attachment is rendered
	imgEl := alicePage.Locator(".message-attachment img")
	err = imgEl.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	src, err := imgEl.GetAttribute("src")
	require.NoError(t, err)
	require.True(t, strings.Contains(src, "/api/images/"))
}

func TestE2EPasteFile(t *testing.T) {
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

	// Open Town Hall chat
	err = alicePage.Locator(".chat-item:has-text(\"Town Hall\")").Click()
	require.NoError(t, err)

	// Programmatically dispatch paste event with plain text file
	pasteJs := `() => {
		const textarea = document.getElementById('message-input');
		const blob = new Blob(['pasted file content bytes'], { type: 'text/plain' });
		const file = new File([blob], 'clipboard_document.txt', { type: 'text/plain' });
		const dt = new DataTransfer();
		dt.items.add(file);
		const ev = new ClipboardEvent('paste', {
			clipboardData: dt,
			bubbles: true,
			cancelable: true
		});
		textarea.dispatchEvent(ev);
	}`

	_, err = alicePage.Evaluate(pasteJs)
	require.NoError(t, err)

	// Verify attachment indicator appears
	err = alicePage.Locator(".attach-indicator").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Send message
	err = alicePage.Locator("#message-input").Fill("Sending my pasted document!")
	require.NoError(t, err)
	err = alicePage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Verify attachment appears in chat
	attachmentEl := alicePage.Locator(".message-attachment-file[data-name='clipboard_document.txt']")
	err = attachmentEl.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Click to open download menu
	err = attachmentEl.Click()
	require.NoError(t, err)

	// Wait for download link
	err = alicePage.Locator("#file-download-menu a").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Download file
	download, err := alicePage.ExpectDownload(func() error {
		return alicePage.Locator("#file-download-menu a").Click()
	})
	require.NoError(t, err)

	require.Equal(t, "clipboard_document.txt", download.SuggestedFilename())
}
