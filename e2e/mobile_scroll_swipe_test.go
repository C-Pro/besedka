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

func TestE2EMobileScrollSwipe(t *testing.T) {
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	aliceSetupLink := server.CreateUser(t, "alice_mobile")

	// Create mobile context
	context, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport: &playwright.Size{
			Width:  375,
			Height: 667,
		},
		IsMobile: playwright.Bool(true),
		HasTouch: playwright.Bool(true),
	})
	require.NoError(t, err)
	defer func() { _ = context.Close() }()

	page, err := context.NewPage()
	require.NoError(t, err)

	// Register / Login
	t.Log("Registering user on mobile...")
	registerUserWithReplace(t, page, aliceSetupLink, "Alice Mobile", "password123", false)

	// On mobile, the app defaults to chat-list or auto-selects chat-window. Let's wait for layout.
	require.Eventually(t, func() bool {
		listVisible, _ := page.Locator(".chat-item:has-text(\"Town Hall\")").IsVisible()
		windowVisible, _ := page.Locator("#chat-area").IsVisible()
		return listVisible || windowVisible
	}, 10*time.Second, 200*time.Millisecond)

	windowVisible, _ := page.Locator("#chat-area").IsVisible()
	if !windowVisible {
		t.Log("On chat-list, clicking Town Hall...")
		_ = page.Locator(".chat-item:has-text(\"Town Hall\")").DispatchEvent("click", nil)
	} else {
		t.Log("Already on chat-window (auto-selected).")
	}

	// Verify we are on chat-window
	t.Log("Waiting for chat-area to become visible...")
	require.Eventually(t, func() bool {
		visible, _ := page.Locator("#chat-area").IsVisible()
		return visible
	}, 5*time.Second, 100*time.Millisecond)

	// Send 10 messages with images
	t.Log("Sending 10 messages with images...")
	for i := 0; i < 10; i++ {
		// Attach image
		err = page.Locator("#file-input").SetInputFiles([]string{"../static/screenshot_mobile.webp"})
		require.NoError(t, err)

		// Wait for attached indicator
		require.Eventually(t, func() bool {
			visible, _ := page.Locator(".attach-indicator").IsVisible()
			return visible
		}, 5*time.Second, 100*time.Millisecond)

		msg := fmt.Sprintf("msg_%02d_xxxxxxxxxxxxxxxxxxxxxx", i)
		err = page.Locator("#message-input").Fill(msg)
		require.NoError(t, err)
		err = page.Locator("#send-btn").Click()
		require.NoError(t, err)

		// Wait for indicator to disappear
		require.Eventually(t, func() bool {
			visible, _ := page.Locator(".attach-indicator").IsVisible()
			return !visible
		}, 5*time.Second, 100*time.Millisecond)
	}

	// Wait until the last message is visible in the container
	t.Log("Waiting for message 09 to appear in DOM...")
	require.Eventually(t, func() bool {
		content, _ := page.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "msg_09_")
	}, 5*time.Second, 200*time.Millisecond)

	// Helper to check if scroll position is at the bottom
	isAtBottom := func() bool {
		res, err := page.Evaluate(`() => {
			const c = document.querySelector('#messages-container');
			return c && (c.scrollHeight - c.scrollTop - c.clientHeight) < 50;
		}`)
		if err != nil {
			return false
		}
		v, ok := res.(bool)
		return ok && v
	}

	require.Eventually(t, isAtBottom, 5*time.Second, 100*time.Millisecond, "Scroll position should be at the bottom after sending 30 messages")

	// Swipe right to the map view (info-panel). Physically, dragging finger left moves view to info-panel.
	t.Log("Swiping to map view (info-panel)...")
	_, err = page.Evaluate(`() => {
		const startEvent = new Event('touchstart');
		startEvent.changedTouches = [{ screenX: 300, screenY: 300 }];
		document.dispatchEvent(startEvent);

		const endEvent = new Event('touchend');
		endEvent.changedTouches = [{ screenX: 50, screenY: 300 }]; // distanceX = -250 -> Swipe Left -> info-panel
		document.dispatchEvent(endEvent);
	}`)
	require.NoError(t, err)

	// Verify info-panel is visible
	require.Eventually(t, func() bool {
		visible, _ := page.Locator("#info-panel").IsVisible()
		return visible
	}, 5*time.Second, 100*time.Millisecond, "Info panel should be visible after swipe")

	// Wait a moment on map view
	time.Sleep(500 * time.Millisecond)

	// Swipe left back to the chat. Physically, dragging finger right moves view to chat-window.
	t.Log("Swiping back to chat window...")
	_, err = page.Evaluate(`() => {
		const startEvent = new Event('touchstart');
		startEvent.changedTouches = [{ screenX: 50, screenY: 300 }];
		document.dispatchEvent(startEvent);

		const endEvent = new Event('touchend');
		endEvent.changedTouches = [{ screenX: 300, screenY: 300 }]; // distanceX = 250 -> Swipe Right -> chat-window
		document.dispatchEvent(endEvent);
	}`)
	require.NoError(t, err)

	// Verify chat-area is visible again
	require.Eventually(t, func() bool {
		visible, _ := page.Locator("#chat-area").IsVisible()
		return visible
	}, 5*time.Second, 100*time.Millisecond, "Chat area should be visible after swiping back")

	// Verify scroll position is still at the bottom
	t.Log("Verifying scroll position remains at the bottom...")
	require.Eventually(t, isAtBottom, 5*time.Second, 100*time.Millisecond, "Scroll position should still be at the bottom after swipe back and forth")
}
