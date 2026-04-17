//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

func TestE2EPushNotifications(t *testing.T) {
	server := startServer(t)
	defer server.Stop()

	pw, browser := setupPlaywright(t)
	defer func() { _ = pw.Stop() }()
	defer func() { _ = browser.Close() }()

	// 1. Create user Alice
	aliceSetupLink := server.CreateUser(t, "alice")

	// 2. Register Alice with notification permission
	aliceContext, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Permissions: []string{"notifications"},
	})
	require.NoError(t, err)
	alicePage, err := aliceContext.NewPage()
	require.NoError(t, err)
	registerUser(t, alicePage, aliceSetupLink, "Alice Smith", "password123")

	// 3. Wait for Service Worker to register
	require.Eventually(t, func() bool {
		return len(aliceContext.ServiceWorkers()) > 0
	}, 10*time.Second, 500*time.Millisecond, "Service Worker should be registered")

	worker := aliceContext.ServiceWorkers()[0]
	require.Contains(t, worker.URL(), "sw.js")

	// 4. Inject spy into the Service Worker to intercept showNotification
	_, err = worker.Evaluate(`() => {
		self.notificationSpy = [];
		const originalShow = self.registration.showNotification;
		self.registration.showNotification = function(title, options) {
			self.notificationSpy.push({title, options});
			return originalShow.apply(this, arguments);
		};
	}`)
	require.NoError(t, err)

	// 5. Dispatch a synthetic PushEvent
	_, err = worker.Evaluate(`() => {
		const payload = JSON.stringify({
			title: 'Push Title',
			body: 'Push Body',
			url: '/?chat=townhall'
		});
		const event = new PushEvent('push', {
			data: payload
		});
		self.dispatchEvent(event);
	}`)
	require.NoError(t, err)

	// 6. Assert that notification was shown
	require.Eventually(t, func() bool {
		spy, err := worker.Evaluate(`() => self.notificationSpy`)
		if err != nil {
			return false
		}
		notifications := spy.([]interface{})
		if len(notifications) == 0 {
			return false
		}
		n := notifications[0].(map[string]interface{})
		options := n["options"].(map[string]interface{})
		return n["title"] == "Push Title" && options["body"] == "Push Body"
	}, 5*time.Second, 200*time.Millisecond, "Notification should be displayed by SW")
}
