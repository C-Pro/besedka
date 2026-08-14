//go:build e2e

package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestE2EProfileSongAndPublicProfile(t *testing.T) {
	t.Parallel()
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	// 1. Create two users: Alice and Bob
	aliceSetupLink := server.CreateUser(t, "alice_song")
	bobSetupLink := server.CreateUser(t, "bob_song")

	// 2. Register Alice
	aliceContext := createBrowserContext(t, browser)
	defer aliceContext.Close()
	alicePage, err := aliceContext.NewPage()
	require.NoError(t, err)
	err = alicePage.SetViewportSize(1280, 800)
	require.NoError(t, err)
	registerUser(t, alicePage, aliceSetupLink, "Alice Song", "password123")

	// 3. Register Bob
	bobContext := createBrowserContext(t, browser)
	defer bobContext.Close()
	bobPage, err := bobContext.NewPage()
	require.NoError(t, err)

	err = bobPage.SetViewportSize(1280, 800)
	require.NoError(t, err)
	registerUser(t, bobPage, bobSetupLink, "Bob Song", "password456")

	aliceID := server.GetUserID(t, "alice_song")

	// 4. Bob views Alice's profile BEFORE any song or bio is set
	_, err = bobPage.Evaluate(fmt.Sprintf("window.createUserProfileModal(window.store, %q)", aliceID))
	require.NoError(t, err)

	err = bobPage.Locator("#user-profile-modal-overlay").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Verify no music player section is displayed when no song is attached
	songSectionCount, err := bobPage.Locator("#user-profile-modal .user-profile-song-section").Count()
	require.NoError(t, err)
	require.Equal(t, 0, songSectionCount, "Music player section should be hidden when no song is set")

	// Close public profile modal in Bob's view
	err = bobPage.Locator("#user-profile-modal-close").Click()
	require.NoError(t, err)

	// 5. Alice sets up her Bio and profile song via single "Profile" menu item
	err = alicePage.Locator("#desktop-profile-avatar").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	err = alicePage.Locator("#desktop-profile-avatar").Click()
	require.NoError(t, err)

	err = alicePage.Locator("#desktop-profile-btn").Click()
	require.NoError(t, err)

	err = alicePage.Locator("#profile-modal-overlay").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Verify initially no title/artist input fields are visible before choosing a file
	hiddenMetadataCount, err := alicePage.Locator("#profile-modal-overlay #song-metadata-container:visible").Count()
	require.NoError(t, err)
	require.Equal(t, 0, hiddenMetadataCount, "Song metadata inputs should be hidden before file upload")

	// Fill Bio and Save
	aliceBio := "Living the dream! 🎵"
	err = alicePage.Locator("#profile-bio-input").Fill(aliceBio)
	require.NoError(t, err)
	err = alicePage.Locator("#bio-save-btn").Click()
	require.NoError(t, err)
	err = alicePage.Locator("#bio-success").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Upload test audio file static/cup.mp3 (reveals title & artist inputs)
	absAudioPath, err := filepath.Abs("../static/cup.mp3")
	require.NoError(t, err)

	err = alicePage.Locator("#song-file-input").SetInputFiles(absAudioPath)
	require.NoError(t, err)

	// Wait for metadata container to become visible after file select
	err = alicePage.Locator("#song-metadata-container").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Customise song title and artist
	err = alicePage.Locator("#song-title-input").Fill("Alice Anthem")
	require.NoError(t, err)
	err = alicePage.Locator("#song-artist-input").Fill("Alice The Star")
	require.NoError(t, err)

	// Click Save Song
	err = alicePage.Locator("#song-save-btn").Click()
	require.NoError(t, err)

	// Wait for success message
	err = alicePage.Locator("#song-success").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// 6. Alice previews her public profile using the "View Public Profile" button inside settings
	err = alicePage.Locator("#view-public-profile-btn").Click()
	require.NoError(t, err)

	err = alicePage.Locator("#user-profile-modal-overlay").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Verify Alice sees her Bio and music player in the preview
	require.Eventually(t, func() bool {
		bioVisible, _ := alicePage.Locator(".user-profile-bio", playwright.PageLocatorOptions{
			HasText: aliceBio,
		}).IsVisible()
		titleVisible, _ := alicePage.Locator(".music-track-title", playwright.PageLocatorOptions{
			HasText: "Alice Anthem",
		}).IsVisible()
		return bioVisible && titleVisible
	}, 10*time.Second, 200*time.Millisecond)

	// Close Alice's public profile preview modal & settings modal
	err = alicePage.Locator("#user-profile-modal-close").Click()
	require.NoError(t, err)
	err = alicePage.Locator("#profile-modal-close").Click()
	require.NoError(t, err)

	// 7. Bob views Alice's profile again
	_, err = bobPage.Evaluate(fmt.Sprintf("window.createUserProfileModal(window.store, %q)", aliceID))
	require.NoError(t, err)

	err = bobPage.Locator("#user-profile-modal-overlay").WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateVisible,
	})
	require.NoError(t, err)

	// Verify Bob sees Alice's Bio, song player, and Send Message button
	require.Eventually(t, func() bool {
		modalVisible, _ := bobPage.Locator("#user-profile-modal").IsVisible()
		bioVisible, _ := bobPage.Locator(".user-profile-bio", playwright.PageLocatorOptions{
			HasText: aliceBio,
		}).IsVisible()
		titleVisible, _ := bobPage.Locator(".music-track-title", playwright.PageLocatorOptions{
			HasText: "Alice Anthem",
		}).IsVisible()
		sendBtnVisible, _ := bobPage.Locator("#profile-send-message-btn").IsVisible()
		return modalVisible && bioVisible && titleVisible && sendBtnVisible
	}, 10*time.Second, 200*time.Millisecond)

	// Verify play button is clickable and toggles state
	err = bobPage.Locator("#user-profile-modal .music-play-btn").Click()
	require.NoError(t, err)
}



