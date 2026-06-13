//go:build e2e

package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

// Two distinct 1x1 PNGs so that each upload yields a distinct fileId (the
// server keys files by content hash), giving us a navigable gallery.
const (
	transparentPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="
	redPNG         = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
)

// TestE2EInfoPanelLastImageOverlay verifies that the info panel's "Last Image"
// preview shows the most recent chat image, that clicking it opens the image
// overlay on that image, and that the overlay can be navigated with the arrow
// keys (clamping at the ends) and closed with Escape.
func TestE2EInfoPanelLastImageOverlay(t *testing.T) {
	t.Parallel()
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

	// Open Town Hall chat (a fresh group chat with no images yet).
	err = alicePage.Locator(".chat-item:has-text(\"Town Hall\")").Click()
	require.NoError(t, err)

	// Helper: paste an image (base64 PNG) into the input and send it.
	sendImage := func(b64, caption string) {
		pasteJs := fmt.Sprintf(`() => {
			const textarea = document.getElementById('message-input');
			const binaryString = atob('%s');
			const bytes = new Uint8Array(binaryString.length);
			for (let i = 0; i < binaryString.length; i++) {
				bytes[i] = binaryString.charCodeAt(i);
			}
			const blob = new Blob([bytes], { type: 'image/png' });
			const file = new File([blob], 'img.png', { type: 'image/png' });
			const dt = new DataTransfer();
			dt.items.add(file);
			textarea.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }));
		}`, b64)
		_, err := alicePage.Evaluate(pasteJs)
		require.NoError(t, err)

		// Wait for the upload to finish (queued-attachment indicator appears).
		err = alicePage.Locator(".attach-indicator").WaitFor(playwright.LocatorWaitForOptions{
			State: playwright.WaitForSelectorStateVisible,
		})
		require.NoError(t, err)

		err = alicePage.Locator("#message-input").Fill(caption)
		require.NoError(t, err)
		err = alicePage.Locator("#send-btn").Click()
		require.NoError(t, err)
	}

	// Send the first image and wait for it to render before sending the
	// second, so the gallery order is deterministic.
	sendImage(transparentPNG, "first image")
	require.Eventually(t, func() bool {
		count, _ := alicePage.Locator(".message-attachment").Count()
		return count == 1
	}, 5*time.Second, 100*time.Millisecond)

	sendImage(redPNG, "second image")
	require.Eventually(t, func() bool {
		count, _ := alicePage.Locator(".message-attachment").Count()
		return count == 2
	}, 5*time.Second, 100*time.Millisecond)

	// --- 1. The latest image's thumbnail is shown in the info panel ---
	err = alicePage.Locator("#last-image-box.has-image").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	lastThumb := alicePage.Locator("#last-image-box img")
	err = lastThumb.WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	lastThumbSrc, err := lastThumb.GetAttribute("src")
	require.NoError(t, err)
	require.Contains(t, lastThumbSrc, "/api/images/")
	require.Contains(t, lastThumbSrc, "thumb=1")

	// The preview must be the *latest* image: its src matches the last
	// message attachment's thumbnail src.
	attCount, err := alicePage.Locator(".message-attachment img").Count()
	require.NoError(t, err)
	latestAttSrc, err := alicePage.Locator(".message-attachment img").Nth(attCount - 1).GetAttribute("src")
	require.NoError(t, err)
	require.Equal(t, latestAttSrc, lastThumbSrc)

	// --- 2. Clicking the preview opens the overlay on the latest image ---
	err = alicePage.Locator("#last-image-box").Click()
	require.NoError(t, err)

	err = alicePage.Locator("#image-overlay.active").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	overlayImg := alicePage.Locator("#overlay-image")
	latestOverlaySrc, err := overlayImg.GetAttribute("src")
	require.NoError(t, err)
	require.Contains(t, latestOverlaySrc, "/api/images/")
	require.NotContains(t, latestOverlaySrc, "thumb=1") // overlay shows full-res
	// The overlay opened on the same (latest) image the thumbnail previewed.
	require.Equal(t, latestOverlaySrc+"?thumb=1", lastThumbSrc)

	// --- 3. Arrow-key navigation between the loaded images ---
	// ArrowLeft -> previous (the first, older image).
	err = alicePage.Keyboard().Press("ArrowLeft")
	require.NoError(t, err)
	var prevOverlaySrc string
	require.Eventually(t, func() bool {
		prevOverlaySrc, _ = overlayImg.GetAttribute("src")
		return prevOverlaySrc != "" && prevOverlaySrc != latestOverlaySrc
	}, 3*time.Second, 100*time.Millisecond)
	require.Contains(t, prevOverlaySrc, "/api/images/")

	// ArrowRight -> back to the latest image.
	err = alicePage.Keyboard().Press("ArrowRight")
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		src, _ := overlayImg.GetAttribute("src")
		return src == latestOverlaySrc
	}, 3*time.Second, 100*time.Millisecond)

	// Navigation clamps at the last image: another ArrowRight does nothing.
	err = alicePage.Keyboard().Press("ArrowRight")
	require.NoError(t, err)
	require.Never(t, func() bool {
		src, _ := overlayImg.GetAttribute("src")
		return src != latestOverlaySrc
	}, 500*time.Millisecond, 100*time.Millisecond)

	// --- 4. Escape closes the overlay ---
	err = alicePage.Keyboard().Press("Escape")
	require.NoError(t, err)
	err = alicePage.Locator("#image-overlay.active").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateHidden,
	})
	require.NoError(t, err)
}
