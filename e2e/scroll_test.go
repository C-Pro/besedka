//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestE2EScrollPosition(t *testing.T) {
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

	// Wait for default load of Town Hall
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".chat-header h3").InnerHTML()
		return strings.Contains(content, "Town Hall")
	}, 5*time.Second, 200*time.Millisecond)

	// Send 30 messages to fill the screen
	for i := 1; i <= 30; i++ {
		err = alicePage.Locator("#message-input").Fill(fmt.Sprintf("msg_%d", i))
		require.NoError(t, err)
		err = alicePage.Locator("#send-btn").Click()
		require.NoError(t, err)
	}

	// Wait until msg_30 is visible
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "msg_30")
	}, 5*time.Second, 200*time.Millisecond)

	// Rule 2: Keep at bottom as new messages arrive
	// Check if scrollTop is near scrollHeight
	isAtBottom := func() bool {
		res, err := alicePage.Evaluate(`() => {
			const c = document.querySelector('#messages-container');
			return (c.scrollHeight - c.scrollTop - c.clientHeight) < 50;
		}`)
		if err != nil {
			return false
		}
		v, ok := res.(bool)
		return ok && v
	}

	require.Eventually(t, isAtBottom, 5*time.Second, 100*time.Millisecond)

	// User scrolls back
	_, err = alicePage.Evaluate(`() => {
		const c = document.querySelector('#messages-container');
		c.scrollTop = 0;
		c.dispatchEvent(new Event('scroll'));
	}`)
	require.NoError(t, err)

	// Wait for scroll event to process
	time.Sleep(500 * time.Millisecond)

	// Another message arrives (we send it via Alice but Alice is scrolled up)
	// Actually if Alice sends it, the text input forces scroll to bottom in the app.
	// So let's create Bob to send the message.
	bobSetupLink := server.CreateUser(t, "bob")
	bobContext := createBrowserContext(t, browser)
	bobPage, err := bobContext.NewPage()
	require.NoError(t, err)
	registerUser(t, bobPage, bobSetupLink, "Bob Jones", "password123")

	require.Eventually(t, func() bool {
		content, _ := bobPage.Locator(".chat-header h3").InnerHTML()
		return strings.Contains(content, "Town Hall")
	}, 5*time.Second, 200*time.Millisecond)

	err = bobPage.Locator("#message-input").Fill("bob_msg_1")
	require.NoError(t, err)
	err = bobPage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Check Alice page received it
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "bob_msg_1")
	}, 5*time.Second, 200*time.Millisecond)

	// Rule 1: Do not override position if user scrolled back
	// Alice should still be near top
	res, err := alicePage.Evaluate(`() => {
		return document.querySelector('#messages-container').scrollTop < 500;
	}`)
	require.NoError(t, err)
	require.True(t, res.(bool), "Alice should remain scrolled up")

	// Rule 3: Navigate to chat page from somewhere else - navigate to last message
	// Bob sends DM to Alice so Alice has another chat to switch to
	err = bobPage.Locator(".chat-item").Filter(playwright.LocatorFilterOptions{HasText: "Alice Smith"}).Click()
	require.NoError(t, err)
	err = bobPage.Locator("#message-input").Fill("bob_dm_1")
	require.NoError(t, err)
	err = bobPage.Locator("#send-btn").Click()
	require.NoError(t, err)

	// Alice switches to Bob DM
	err = alicePage.Locator(".chat-item").Filter(playwright.LocatorFilterOptions{HasText: "Bob Jones"}).Click()
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".chat-header h3").InnerHTML()
		return strings.Contains(content, "Bob Jones")
	}, 5*time.Second, 200*time.Millisecond)

	require.Eventually(t, isAtBottom, 5*time.Second, 100*time.Millisecond, "Should be at bottom after switching to DM")

	// Alice switches BACK to Town Hall
	err = alicePage.Locator(".chat-item").Filter(playwright.LocatorFilterOptions{HasText: "Town Hall"}).Click()
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".chat-header h3").InnerHTML()
		return strings.Contains(content, "Town Hall")
	}, 5*time.Second, 200*time.Millisecond)

	// Should be at bottom for Town Hall too!
	require.Eventually(t, isAtBottom, 5*time.Second, 100*time.Millisecond, "Should be at bottom after navigating back to Town Hall")
}

func TestE2EScroll_BotProgressCard(t *testing.T) {
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

	// Wait for default load of Town Hall
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".chat-header h3").InnerHTML()
		return strings.Contains(content, "Town Hall")
	}, 5*time.Second, 200*time.Millisecond)

	// Create bot user
	botKey := server.CreateBotAPI(t, "test_bot", "Test Bot")

	// Send messages to fill the screen and make the container scrollable
	for i := 1; i <= 20; i++ {
		err = alicePage.Locator("#message-input").Fill(fmt.Sprintf("prep_msg_%d", i))
		require.NoError(t, err)
		err = alicePage.Locator("#send-btn").Click()
		require.NoError(t, err)
	}

	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".messages-container").InnerHTML()
		return strings.Contains(content, "prep_msg_20")
	}, 5*time.Second, 200*time.Millisecond)

	isAtBottom := func() bool {
		res, err := alicePage.Evaluate(`() => {
			const c = document.querySelector('#messages-container');
			return (c.scrollHeight - c.scrollTop - c.clientHeight) < 50;
		}`)
		if err != nil {
			return false
		}
		v, ok := res.(bool)
		return ok && v
	}

	require.Eventually(t, isAtBottom, 5*time.Second, 100*time.Millisecond)

	// Bot sends root progress card
	rootBody, err := json.Marshal(map[string]any{
		"messageType": "progress",
		"progress": map[string]any{
			"title": "Long running bot task",
		},
	})
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/chats/townhall/messages", server.APIAddr), bytes.NewReader(rootBody))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+botKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var rootResp struct {
		Seq int64 `json:"seq"`
	}
	err = json.NewDecoder(resp.Body).Decode(&rootResp)
	require.NoError(t, err)

	// Wait for progress card to appear
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".progress-card-title").InnerHTML()
		return strings.Contains(content, "Long running bot task")
	}, 5*time.Second, 200*time.Millisecond)

	require.Eventually(t, isAtBottom, 5*time.Second, 100*time.Millisecond)

	// Bot appends multiple progress steps to the progress card
	for i := 1; i <= 6; i++ {
		stepBody, err := json.Marshal(map[string]any{
			"messageType": "progress",
			"progress": map[string]any{
				"parentSeq": rootResp.Seq,
				"cardStatus": "running",
				"step": map[string]any{
					"id":          fmt.Sprintf("step_%d", i),
					"title":       fmt.Sprintf("Step %d: Performing subtask %d", i, i),
					"description": fmt.Sprintf("Processing details for step %d to increase element height.", i),
					"status":      "completed",
				},
			},
		})
		require.NoError(t, err)

		stepReq, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/chats/townhall/messages", server.APIAddr), bytes.NewReader(stepBody))
		require.NoError(t, err)
		stepReq.Header.Set("Authorization", "Bearer "+botKey)
		stepReq.Header.Set("Content-Type", "application/json")
		stepResp, err := http.DefaultClient.Do(stepReq)
		require.NoError(t, err)
		_ = stepResp.Body.Close()
		require.Equal(t, http.StatusOK, stepResp.StatusCode)
	}

	// Wait until step 6 is in the DOM
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".progress-timeline").InnerHTML()
		return strings.Contains(content, "Step 6")
	}, 5*time.Second, 200*time.Millisecond)

	// Verify that the UI scrolled down to keep the new progress messages in view
	require.Eventually(t, isAtBottom, 5*time.Second, 100*time.Millisecond, "UI should scroll down to bottom as new progress messages are appended")

	// Also verify that the last step element is actually visible in the viewport
	isStepVisible := func() bool {
		res, err := alicePage.Evaluate(`() => {
			const c = document.querySelector('#messages-container');
			const step = c.querySelector('[data-step-id="step_6"]');
			if (!step) return false;
			const cRect = c.getBoundingClientRect();
			const stepRect = step.getBoundingClientRect();
			return stepRect.bottom <= cRect.bottom + 10 && stepRect.top >= cRect.top - 10;
		}`)
		if err != nil {
			return false
		}
		v, ok := res.(bool)
		return ok && v
	}
	require.Eventually(t, isStepVisible, 5*time.Second, 100*time.Millisecond, "Newly appended progress step should be visible in viewport")

	// Verify Rule 1: If user scrolls back up, new progress updates do not override scroll position
	_, err = alicePage.Evaluate(`() => {
		const c = document.querySelector('#messages-container');
		c.scrollTop = 0;
		c.dispatchEvent(new Event('scroll'));
	}`)
	require.NoError(t, err)

	time.Sleep(300 * time.Millisecond)

	// Bot appends step 7 while Alice is scrolled up
	step7Body, err := json.Marshal(map[string]any{
		"messageType": "progress",
		"progress": map[string]any{
			"parentSeq": rootResp.Seq,
			"cardStatus": "running",
			"step": map[string]any{
				"id":          "step_7",
				"title":       "Step 7: Background task while scrolled up",
				"description": "User is scrolled up reading earlier messages.",
				"status":      "completed",
			},
		},
	})
	require.NoError(t, err)

	step7Req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/api/chats/townhall/messages", server.APIAddr), bytes.NewReader(step7Body))
	require.NoError(t, err)
	step7Req.Header.Set("Authorization", "Bearer "+botKey)
	step7Req.Header.Set("Content-Type", "application/json")
	step7Resp, err := http.DefaultClient.Do(step7Req)
	require.NoError(t, err)
	_ = step7Resp.Body.Close()
	require.Equal(t, http.StatusOK, step7Resp.StatusCode)

	// Wait for step 7 to be in DOM
	require.Eventually(t, func() bool {
		content, _ := alicePage.Locator(".progress-timeline").InnerHTML()
		return strings.Contains(content, "Step 7")
	}, 5*time.Second, 200*time.Millisecond)

	// Alice should still remain scrolled up
	res, err := alicePage.Evaluate(`() => {
		return document.querySelector('#messages-container').scrollTop < 500;
	}`)
	require.NoError(t, err)
	require.True(t, res.(bool), "Alice should remain scrolled up when bot appends progress while reading history")
}
