//go:build e2e

package e2e

import (
	"besedka/internal/auth"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/stretchr/testify/require"
)

type TestServer struct {
	APIAddr   string
	AdminAddr string
	BaseURL   string
	DBPath    string
	Cmd       *exec.Cmd
}

func getFreePort(t *testing.T) int {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	require.NoError(t, err)

	l, err := net.ListenTCP("tcp", addr)
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

func startServer(t *testing.T) *TestServer {
	apiPort := getFreePort(t)
	adminPort := getFreePort(t)
	apiAddr := fmt.Sprintf("localhost:%d", apiPort)
	adminAddr := fmt.Sprintf("localhost:%d", adminPort)
	baseURL := fmt.Sprintf("http://%s", apiAddr)

	tmpDB, err := os.CreateTemp("", "besedka-e2e-*.db")
	require.NoError(t, err)
	dbPath := tmpDB.Name()
	_ = tmpDB.Close()

	cmd := exec.Command(serverBinPath)
	cmd.Env = append(os.Environ(),
		"AUTH_SECRET=test-secret-key-must-be-long-enough-for-base64-if-needed",
		fmt.Sprintf("API_ADDR=%s", apiAddr),
		fmt.Sprintf("ADMIN_ADDR=%s", adminAddr),
		fmt.Sprintf("BASE_URL=%s", baseURL),
		fmt.Sprintf("BESEDKA_DB=%s", dbPath),
	)

	// Redirect output to stdout/stderr for debugging if needed
	// cmd.Stdout = os.Stdout
	// cmd.Stderr = os.Stderr

	err = cmd.Start()
	require.NoError(t, err)

	// Wait for server to be ready
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", apiAddr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		return false
	}, 5*time.Second, 200*time.Millisecond, "Server failed to start")

	return &TestServer{
		APIAddr:   apiAddr,
		AdminAddr: adminAddr,
		BaseURL:   baseURL,
		DBPath:    dbPath,
		Cmd:       cmd,
	}
}

func (s *TestServer) Stop() {
	if s.Cmd != nil && s.Cmd.Process != nil {
		_ = s.Cmd.Process.Kill()
	}
	if s.DBPath != "" {
		_ = os.Remove(s.DBPath)
		_ = os.Remove(s.DBPath + "-lock") // bbolt lock file
	}
}

func (s *TestServer) CreateUser(t *testing.T, username string) string {
	cmd := exec.Command(serverBinPath, "-add-user", username)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("ADMIN_ADDR=%s", s.AdminAddr),
		fmt.Sprintf("BASE_URL=%s", s.BaseURL),
	)

	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "Failed to create user via CLI: %s", string(output))

	// Extract Setup Link: http://...
	re := regexp.MustCompile(`Setup Link:\s+(http://\S+)`)
	matches := re.FindStringSubmatch(string(output))
	require.Len(t, matches, 2, "Could not find setup link in output: %s", string(output))

	return matches[1]
}

func getTOTP(t *testing.T, secret string) string {
	code, err := auth.GenerateTOTP(secret, time.Now())
	require.NoError(t, err)
	return fmt.Sprintf("%06d", code)
}

func setupPlaywright(t *testing.T) (*playwright.Playwright, playwright.Browser) {
	pw, err := playwright.Run()
	require.NoError(t, err)

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	require.NoError(t, err)

	return pw, browser
}

func createBrowserContext(t *testing.T, browser playwright.Browser) playwright.BrowserContext {
	context, err := browser.NewContext()
	require.NoError(t, err)
	return context
}
