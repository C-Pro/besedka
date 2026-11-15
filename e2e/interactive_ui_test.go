//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
	"github.com/stretchr/testify/require"
)

func createBotWithMentionsAPI(t *testing.T, s *TestServer, username, displayName string) string {
	t.Helper()
	reqBody, err := json.Marshal(map[string]any{
		"username":    username,
		"displayName": displayName,
		"type":        "bot",
		"botPermissions": map[string]any{
			"write":        true,
			"readMentions": true,
		},
	})
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/users", s.AdminAddr), bytes.NewReader(reqBody))
	require.NoError(t, err)
	req.SetBasicAuth("admin", "1337chat")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Success bool   `json:"success"`
		APIKey  string `json:"apiKey"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.True(t, result.Success)
	require.NotEmpty(t, result.APIKey)
	return result.APIKey
}

func postBotMessageAPI(t *testing.T, baseURL, botKey string, payload map[string]any) int64 {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/chats/townhall/messages", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+botKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var res struct {
		Seq int64 `json:"seq"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&res))
	return res.Seq
}

func TestE2EInteractiveUI(t *testing.T) {
	t.Parallel()
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	botKey := createBotWithMentionsAPI(t, server, "deploybot", "Deploy Bot")
	aliceLink := server.CreateUser(t, "alice")
	bobLink := server.CreateUser(t, "bob")

	aliceCtx := createBrowserContext(t, browser)
	alicePage, err := aliceCtx.NewPage()
	require.NoError(t, err)
	registerUser(t, alicePage, aliceLink, "Alice", "password123")
	selectTownhall(t, alicePage)

	bobCtx := createBrowserContext(t, browser)
	bobPage, err := bobCtx.NewPage()
	require.NoError(t, err)
	registerUser(t, bobPage, bobLink, "Bob", "password456")
	selectTownhall(t, bobPage)

	// 1. Bot sends approval prompt with explicit primary/danger buttons and Markdown text
	prompt1Seq := postBotMessageAPI(t, server.BaseURL, botKey, map[string]any{
		"type":    "text",
		"content": "Deploy **v3.0** to production?",
		"ui": map[string]any{
			"elements": []map[string]any{
				{"id": "approve", "type": "button", "label": "Approve", "style": "primary", "description": "Roll out now"},
				{"id": "deny", "type": "button", "label": "Deny", "style": "danger"},
			},
		},
	})

	alicePrompt1 := alicePage.Locator(fmt.Sprintf(`.message-line[data-seq="%d"]`, prompt1Seq))
	bobPrompt1 := bobPage.Locator(fmt.Sprintf(`.message-line[data-seq="%d"]`, prompt1Seq))
	require.NoError(t, alicePrompt1.Locator(".message-ui").WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible}))
	require.NoError(t, bobPrompt1.Locator(".message-ui").WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible}))

	strongText, err := alicePrompt1.Locator(".message-content strong").InnerText()
	require.NoError(t, err)
	require.Equal(t, "v3.0", strongText)

	// 2. Alice clicks Approve -> locks UI live on both Alice & Bob and emits ui_response
	approveBtn := alicePrompt1.Locator(`.message-ui-btn[data-element-id="approve"]`)
	require.NoError(t, approveBtn.Click())

	for _, promptLoc := range []playwright.Locator{alicePrompt1, bobPrompt1} {
		require.Eventually(t, func() bool {
			ans, err := promptLoc.Locator(".message-ui").GetAttribute("data-answered")
			return err == nil && ans == "true"
		}, 5*time.Second, 100*time.Millisecond)

		disabledApprove, err := promptLoc.Locator(`.message-ui-btn[data-element-id="approve"]`).IsDisabled()
		require.NoError(t, err)
		require.True(t, disabledApprove)

		disabledDeny, err := promptLoc.Locator(`.message-ui-btn[data-element-id="deny"]`).IsDisabled()
		require.NoError(t, err)
		require.True(t, disabledDeny)

		hasSelectedClass, err := promptLoc.Locator(`.message-ui-btn[data-element-id="approve"]`).Evaluate(`el => el.classList.contains('is-selected')`, nil)
		require.NoError(t, err)
		require.Equal(t, true, hasSelectedClass)
	}
	waitTownhallMessage(t, alicePage, "@deploybot Selected: Approve")
	waitTownhallMessage(t, bobPage, "@deploybot Selected: Approve")

	// 3. Bot sends checkbox + radio prompt (no explicit button -> automatic Submit)
	prompt2Seq := postBotMessageAPI(t, server.BaseURL, botKey, map[string]any{
		"type":    "text",
		"content": "Choose rollout target:",
		"ui": map[string]any{
			"elements": []map[string]any{
				{"id": "notify", "type": "checkbox", "label": "Notify Slack", "default": true},
				{"id": "us_east", "type": "radio", "label": "US East"},
				{"id": "eu_west", "type": "radio", "label": "EU West", "default": true},
			},
		},
	})

	bobPrompt2 := bobPage.Locator(fmt.Sprintf(`.message-line[data-seq="%d"]`, prompt2Seq))
	alicePrompt2 := alicePage.Locator(fmt.Sprintf(`.message-line[data-seq="%d"]`, prompt2Seq))
	require.NoError(t, bobPrompt2.Locator(".message-ui-submit-btn").WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible}))

	// Default states applied
	notifyChecked, err := bobPrompt2.Locator(`input[data-element-id="notify"]`).IsChecked()
	require.NoError(t, err)
	require.True(t, notifyChecked)
	euChecked, err := bobPrompt2.Locator(`input[data-element-id="eu_west"]`).IsChecked()
	require.NoError(t, err)
	require.True(t, euChecked)

	// Bob unchecks Notify Slack, picks US East, and clicks auto-Submit
	require.NoError(t, bobPrompt2.Locator(`input[data-element-id="notify"]`).Uncheck())
	require.NoError(t, bobPrompt2.Locator(`input[data-element-id="us_east"]`).Check())
	require.NoError(t, bobPrompt2.Locator(".message-ui-submit-btn").Click())

	for _, promptLoc := range []playwright.Locator{alicePrompt2, bobPrompt2} {
		require.Eventually(t, func() bool {
			ans, err := promptLoc.Locator(".message-ui").GetAttribute("data-answered")
			return err == nil && ans == "true"
		}, 5*time.Second, 100*time.Millisecond)

		usChecked, err := promptLoc.Locator(`input[data-element-id="us_east"]`).IsChecked()
		require.NoError(t, err)
		require.True(t, usChecked)

		notifyStillChecked, err := promptLoc.Locator(`input[data-element-id="notify"]`).IsChecked()
		require.NoError(t, err)
		require.False(t, notifyStillChecked)

		usDisabled, err := promptLoc.Locator(`input[data-element-id="us_east"]`).IsDisabled()
		require.NoError(t, err)
		require.True(t, usDisabled)

		submitCount, err := promptLoc.Locator(".message-ui-submit-btn").Count()
		require.NoError(t, err)
		require.Equal(t, 0, submitCount)
	}
	waitTownhallMessage(t, alicePage, "@deploybot Selected: US East")

	// 4. Reload Alice's page -> locked answered states persist cleanly
	_, err = alicePage.Reload()
	require.NoError(t, err)
	require.NoError(t, alicePage.Locator("#app").WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible}))
	selectTownhall(t, alicePage)

	reloadedPrompt1 := alicePage.Locator(fmt.Sprintf(`.message-line[data-seq="%d"]`, prompt1Seq))
	reloadedPrompt2 := alicePage.Locator(fmt.Sprintf(`.message-line[data-seq="%d"]`, prompt2Seq))
	require.NoError(t, reloadedPrompt1.Locator(`.message-ui[data-answered="true"]`).WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible}))
	require.NoError(t, reloadedPrompt2.Locator(`.message-ui[data-answered="true"]`).WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateVisible}))

	reloadedUSChecked, err := reloadedPrompt2.Locator(`input[data-element-id="us_east"]`).IsChecked()
	require.NoError(t, err)
	require.True(t, reloadedUSChecked)

	// 5. Bot polls REST history and receives both ui_response payloads with metadata
	reqHist, err := http.NewRequest(http.MethodGet, server.BaseURL+"/api/chats/townhall/messages?toSeq=100", nil)
	require.NoError(t, err)
	reqHist.Header.Set("Authorization", "Bearer "+botKey)
	respHist, err := http.DefaultClient.Do(reqHist)
	require.NoError(t, err)
	defer func() { _ = respHist.Body.Close() }()
	require.Equal(t, http.StatusOK, respHist.StatusCode)

	var botMsgs []struct {
		Seq        int64  `json:"seq"`
		Type       string `json:"type"`
		RawContent string `json:"rawContent"`
		UIResponse *struct {
			ParentSeq   int64    `json:"parentSeq"`
			SelectedIDs []string `json:"selectedIds"`
		} `json:"uiResponse"`
	}
	require.NoError(t, json.NewDecoder(respHist.Body).Decode(&botMsgs))
	require.Len(t, botMsgs, 2)
	require.Equal(t, "ui_response", botMsgs[0].Type)
	require.NotNil(t, botMsgs[0].UIResponse)
	require.Equal(t, prompt1Seq, botMsgs[0].UIResponse.ParentSeq)
	require.Equal(t, []string{"approve"}, botMsgs[0].UIResponse.SelectedIDs)

	require.Equal(t, "ui_response", botMsgs[1].Type)
	require.NotNil(t, botMsgs[1].UIResponse)
	require.Equal(t, prompt2Seq, botMsgs[1].UIResponse.ParentSeq)
	require.Equal(t, []string{"us_east"}, botMsgs[1].UIResponse.SelectedIDs)
}
