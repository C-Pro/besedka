package main

import (
	"besedka/internal/api"
	"besedka/internal/auth"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIntegration(t *testing.T) {
	// Setup temporary DB and ports
	dbFile := "integration_test.db"
	_ = os.Remove(dbFile) // cleanup before
	defer func() { _ = os.Remove(dbFile) }()

	adminAddr := "127.0.0.1:8888"
	apiAddr := ":8887"

	uploadsDir := t.TempDir()
	_ = os.Setenv("BESEDKA_DB", dbFile)
	_ = os.Setenv("ADMIN_ADDR", adminAddr)
	_ = os.Setenv("API_ADDR", apiAddr)
	_ = os.Setenv("AUTH_SECRET", "very-secure-test-secret")
	_ = os.Setenv("UPLOADS_PATH", uploadsDir)
	defer func() {
		_ = os.Unsetenv("BESEDKA_DB")
		_ = os.Unsetenv("ADMIN_ADDR")
		_ = os.Unsetenv("API_ADDR")
		_ = os.Unsetenv("AUTH_SECRET")
		_ = os.Unsetenv("UPLOADS_PATH")
	}()

	// Start server in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := run(ctx, cliOptions{}); err != nil {
			// run returns context.Canceled on shutdown, ignore it
			if err != context.Canceled {
				t.Errorf("Server error: %v", err)
			}
		}
	}()

	// Wait for server to start
	waitForServer(t, "http://127.0.0.1:8888/admin/users", 50)

	// Step 0: Verify Root Redirect (New Check)
	// Requesting root without token should redirect to login.html with 302 Found
	{
		client := &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // Don't follow redirects automatically
			},
		}
		resp, err := client.Get(fmt.Sprintf("http://localhost%s/", apiAddr))
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusFound, resp.StatusCode)
		location, err := resp.Location()
		require.NoError(t, err)
		require.Equal(t, "/login.html", location.Path)
	}

	// Step 1: Create User via Admin API (Invite)
	username := "testuser"
	reqBody, _ := json.Marshal(api.AddUserRequest{Username: username})
	req, err := http.NewRequest("POST", fmt.Sprintf("http://%s/admin/users", adminAddr), bytes.NewBuffer(reqBody))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("admin", "1337chat")

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)

	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var adminResp api.AddUserResponse
	err = json.NewDecoder(resp.Body).Decode(&adminResp)
	require.NoError(t, err)
	require.True(t, adminResp.Success)
	require.Equal(t, username, adminResp.Username)
	setupLink := adminResp.SetupLink
	require.NotEmpty(t, setupLink)

	// Step 2: Get Registration Info
	// Wait, setupLink is /register.html?token=...
	// We need to parse it.
	u, err := url.Parse(setupLink)
	require.NoError(t, err)
	token := u.Query().Get("token")
	require.NotEmpty(t, token)

	resp, err = http.Get(fmt.Sprintf("http://localhost%s/api/register-info?token=%s", apiAddr, url.QueryEscape(token)))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var regInfo auth.RegistrationInfoResponse
	err = json.NewDecoder(resp.Body).Decode(&regInfo)
	require.NoError(t, err)
	require.Equal(t, username, regInfo.Username)
	totpSecret := regInfo.TOTPSecret
	require.NotEmpty(t, totpSecret)

	// Step 3: Complete Registration
	// Provide password and TOTP code
	newPassword := "securepassword"
	// Generate TOTP code
	totpCode, err := auth.GenerateTOTP(totpSecret, time.Now())
	require.NoError(t, err)

	regReq := auth.RegistrationRequest{
		Token:    token,
		Password: newPassword,
		TOTP:     totpCode,
	}
	regBody, _ := json.Marshal(regReq)
	reqReg, _ := http.NewRequest("POST", fmt.Sprintf("http://localhost%s/api/register", apiAddr), bytes.NewBuffer(regBody))
	reqReg.Header.Set("Content-Type", "application/json")
	reqReg.Header.Set("Origin", fmt.Sprintf("http://localhost%s", apiAddr))
	resp, err = client.Do(reqReg)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Step 4: Login
	loginReq := auth.LoginRequest{
		Username: username,
		Password: newPassword,
		TOTP:     totpCode,
	}

	loginBody, _ := json.Marshal(loginReq)
	reqLogin, _ := http.NewRequest("POST", fmt.Sprintf("http://localhost%s/api/login", apiAddr), bytes.NewBuffer(loginBody))
	reqLogin.Header.Set("Content-Type", "application/json")
	reqLogin.Header.Set("Origin", fmt.Sprintf("http://localhost%s", apiAddr))
	resp, err = client.Do(reqLogin)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var loginResp auth.LoginResponse
	err = json.NewDecoder(resp.Body).Decode(&loginResp)
	require.NoError(t, err)
	require.True(t, loginResp.Success)
	sessionToken := loginResp.Token
	require.NotEmpty(t, sessionToken)

	// Step 4.5: Upload Avatar
	// We simulate an image upload using a minimal valid PNG valid for h2non/filetype
	pngBase64 := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII="
	pngDecoded, err := base64.StdEncoding.DecodeString(pngBase64)
	require.NoError(t, err)

	reqAvatar, err := http.NewRequest("POST", fmt.Sprintf("http://localhost%s/api/users/me/avatar", apiAddr), bytes.NewReader(pngDecoded))
	require.NoError(t, err)
	reqAvatar.Header.Set("Content-Type", "image/png")
	reqAvatar.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	reqAvatar.Header.Set("Origin", fmt.Sprintf("http://localhost%s", apiAddr))

	respAvatar, err := client.Do(reqAvatar)
	require.NoError(t, err)
	defer func() { _ = respAvatar.Body.Close() }()
	require.Equal(t, http.StatusOK, respAvatar.StatusCode)

	var avatarResp struct {
		AvatarURL string `json:"avatarUrl"`
	}
	err = json.NewDecoder(respAvatar.Body).Decode(&avatarResp)
	require.NoError(t, err)
	require.NotEmpty(t, avatarResp.AvatarURL)
	require.Contains(t, avatarResp.AvatarURL, "/api/images/")
	require.Contains(t, avatarResp.AvatarURL, "?thumb=1", "avatar URL should request the thumbnail")

	// The tiny avatar PNG is below the thumbnail threshold, so the thumb URL
	// must fall back to the original.
	reqAvatarThumb, err := http.NewRequest("GET", fmt.Sprintf("http://localhost%s%s", apiAddr, avatarResp.AvatarURL), nil)
	require.NoError(t, err)
	reqAvatarThumb.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	respAvatarThumb, err := client.Do(reqAvatarThumb)
	require.NoError(t, err)
	defer func() { _ = respAvatarThumb.Body.Close() }()
	require.Equal(t, http.StatusOK, respAvatarThumb.StatusCode)
	require.Equal(t, "image/png", respAvatarThumb.Header.Get("Content-Type"))
	var avatarBody bytes.Buffer
	_, err = avatarBody.ReadFrom(respAvatarThumb.Body)
	require.NoError(t, err)
	require.Equal(t, pngDecoded, avatarBody.Bytes(), "small image thumb request should serve the original")

	// Step 4.55: Upload a large image and verify thumbnail serving
	bigPNG := makeNoisePNG(t, 1200, 900)
	require.Greater(t, len(bigPNG), 100*1024, "test image must exceed the thumbnail threshold")

	reqBigImg, err := http.NewRequest("POST", fmt.Sprintf("http://localhost%s/api/upload/image", apiAddr), bytes.NewReader(bigPNG))
	require.NoError(t, err)
	reqBigImg.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	reqBigImg.Header.Set("Origin", fmt.Sprintf("http://localhost%s", apiAddr))
	respBigImg, err := client.Do(reqBigImg)
	require.NoError(t, err)
	defer func() { _ = respBigImg.Body.Close() }()
	require.Equal(t, http.StatusOK, respBigImg.StatusCode)

	var bigImgResp struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(respBigImg.Body).Decode(&bigImgResp))
	require.NotEmpty(t, bigImgResp.ID)

	// Thumbnail request returns a JPEG smaller than the original.
	reqThumb, err := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/images/%s?thumb=1", apiAddr, bigImgResp.ID), nil)
	require.NoError(t, err)
	reqThumb.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	respThumb, err := client.Do(reqThumb)
	require.NoError(t, err)
	defer func() { _ = respThumb.Body.Close() }()
	require.Equal(t, http.StatusOK, respThumb.StatusCode)
	require.Equal(t, "image/jpeg", respThumb.Header.Get("Content-Type"))
	var thumbBody bytes.Buffer
	_, err = thumbBody.ReadFrom(respThumb.Body)
	require.NoError(t, err)
	require.Less(t, thumbBody.Len(), len(bigPNG), "thumbnail should be smaller than the original")
	require.LessOrEqual(t, thumbBody.Len(), 100*1024, "thumbnail should fit the size target")

	// Plain request still returns the full original.
	reqFull, err := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/images/%s", apiAddr, bigImgResp.ID), nil)
	require.NoError(t, err)
	reqFull.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	respFull, err := client.Do(reqFull)
	require.NoError(t, err)
	defer func() { _ = respFull.Body.Close() }()
	require.Equal(t, http.StatusOK, respFull.StatusCode)
	require.Equal(t, "image/png", respFull.Header.Get("Content-Type"))
	var fullBody bytes.Buffer
	_, err = fullBody.ReadFrom(respFull.Body)
	require.NoError(t, err)
	require.Equal(t, bigPNG, fullBody.Bytes(), "full image request should serve the original")

	// Step 4.56: Upload a large WebP image and verify thumbnail serving
	bigWebP := makeLargeWebP(t)
	require.Greater(t, len(bigWebP), 100*1024, "webp test image must exceed the thumbnail threshold")

	reqWebP, err := http.NewRequest("POST", fmt.Sprintf("http://localhost%s/api/upload/image", apiAddr), bytes.NewReader(bigWebP))
	require.NoError(t, err)
	reqWebP.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	reqWebP.Header.Set("Origin", fmt.Sprintf("http://localhost%s", apiAddr))
	respWebP, err := client.Do(reqWebP)
	require.NoError(t, err)
	defer func() { _ = respWebP.Body.Close() }()
	require.Equal(t, http.StatusOK, respWebP.StatusCode)

	var webpResp struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.NewDecoder(respWebP.Body).Decode(&webpResp))
	require.NotEmpty(t, webpResp.ID)

	// Thumbnail request returns a JPEG
	reqWebPThumb, err := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/images/%s?thumb=1", apiAddr, webpResp.ID), nil)
	require.NoError(t, err)
	reqWebPThumb.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	respWebPThumb, err := client.Do(reqWebPThumb)
	require.NoError(t, err)
	defer func() { _ = respWebPThumb.Body.Close() }()
	require.Equal(t, http.StatusOK, respWebPThumb.StatusCode)
	require.Equal(t, "image/jpeg", respWebPThumb.Header.Get("Content-Type"))
	var webpThumbBody bytes.Buffer
	_, err = webpThumbBody.ReadFrom(respWebPThumb.Body)
	require.NoError(t, err)
	require.Less(t, webpThumbBody.Len(), len(bigWebP), "thumbnail should be smaller than original")

	// Plain request returns original WebP
	reqWebPFull, err := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/images/%s", apiAddr, webpResp.ID), nil)
	require.NoError(t, err)
	reqWebPFull.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	respWebPFull, err := client.Do(reqWebPFull)
	require.NoError(t, err)
	defer func() { _ = respWebPFull.Body.Close() }()
	require.Equal(t, http.StatusOK, respWebPFull.StatusCode)
	require.Equal(t, "image/webp", respWebPFull.Header.Get("Content-Type"))
	var webpFullBody bytes.Buffer
	_, err = webpFullBody.ReadFrom(respWebPFull.Body)
	require.NoError(t, err)
	require.Equal(t, bigWebP, webpFullBody.Bytes(), "full image request should serve the original WebP")

	// Step 4.6: Upload and Download File
	fileContent := []byte("hello world this is a test file")
	reqFile, err := http.NewRequest("POST", fmt.Sprintf("http://localhost%s/api/upload/file", apiAddr), bytes.NewReader(fileContent))
	require.NoError(t, err)
	reqFile.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	reqFile.Header.Set("Origin", fmt.Sprintf("http://localhost%s", apiAddr))

	respFile, err := client.Do(reqFile)
	require.NoError(t, err)
	defer func() { _ = respFile.Body.Close() }()
	require.Equal(t, http.StatusOK, respFile.StatusCode)

	var fileResp struct {
		ID string `json:"id"`
	}
	err = json.NewDecoder(respFile.Body).Decode(&fileResp)
	require.NoError(t, err)
	require.NotEmpty(t, fileResp.ID)

	// Download File
	reqGetFile, err := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/files/%s", apiAddr, fileResp.ID), nil)
	require.NoError(t, err)
	reqGetFile.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	respGetFile, err := client.Do(reqGetFile)
	require.NoError(t, err)
	defer func() { _ = respGetFile.Body.Close() }()
	require.Equal(t, http.StatusOK, respGetFile.StatusCode)

	var downloaded bytes.Buffer
	_, err = downloaded.ReadFrom(respGetFile.Body)
	require.NoError(t, err)
	require.Equal(t, fileContent, downloaded.Bytes())

	// Step 5: List Users (Verify Login and Avatar)
	reqUsers, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/users", apiAddr), nil)
	reqUsers.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	respUsers, err := client.Do(reqUsers)
	require.NoError(t, err)
	defer func() { _ = respUsers.Body.Close() }()
	require.Equal(t, http.StatusOK, respUsers.StatusCode)

	var users []struct {
		ID        string `json:"id"`
		AvatarURL string `json:"avatarUrl"`
	}
	err = json.NewDecoder(respUsers.Body).Decode(&users)
	require.NoError(t, err)
	require.NotEmpty(t, users)
	require.Equal(t, avatarResp.AvatarURL, users[0].AvatarURL, "Avatar URL should match the uploaded avatar")
	testUserID := users[0].ID

	// Step 7: Admin Delete User Revokes Tokens

	// Delete user via Admin API
	reqDel, _ := http.NewRequest("DELETE", fmt.Sprintf("http://%s/api/users?id=%s", adminAddr, testUserID), nil)
	reqDel.SetBasicAuth("admin", "1337chat")
	client = &http.Client{}
	respDel, err := client.Do(reqDel)
	require.NoError(t, err)
	defer func() { _ = respDel.Body.Close() }()
	require.Equal(t, http.StatusOK, respDel.StatusCode)

	// Verify Token Revoked
	reqRevoke, _ := http.NewRequest("GET", fmt.Sprintf("http://localhost%s/api/users", apiAddr), nil)
	reqRevoke.AddCookie(&http.Cookie{Name: "token", Value: sessionToken})
	// We need client that doesn't follow redirects to check for 302/401
	noRedirectClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	respRevoke, err := noRedirectClient.Do(reqRevoke)
	require.NoError(t, err)
	defer func() { _ = respRevoke.Body.Close() }()
	// Expect 302 Found (redirect to login) or 401
	if respRevoke.StatusCode != http.StatusFound && respRevoke.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected 302 or 401, got %d", respRevoke.StatusCode)
	}
}

func waitForServer(t *testing.T, urlStr string, retries int) {
	req, _ := http.NewRequest("GET", urlStr, nil)
	req.SetBasicAuth("admin", "1337chat") // Use default creds
	client := &http.Client{Timeout: 500 * time.Millisecond}

	for i := 0; i < retries; i++ {
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			// Accept 200 OK or 401 Unauthorized (invalid auth but server is up)
			// But since we send auth, we expect 200 or at least not connection refused.
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Server failed to start at %s after %d retries", urlStr, retries)
}

// makeNoisePNG builds a PNG filled with deterministic per-pixel noise so the
// encoding stays large enough to exceed the thumbnail threshold.
func makeNoisePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	rnd := rand.New(rand.NewSource(42))
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(rnd.Intn(256)),
				G: uint8(rnd.Intn(256)),
				B: uint8(rnd.Intn(256)),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode png: %v", err)
	}
	return buf.Bytes()
}

// makeLargeWebP builds a dummy WebP image by padding a 1x1 transparent WebP
// with zeros so it exceeds the thumbnail threshold.
func makeLargeWebP(t *testing.T) []byte {
	t.Helper()
	b64 := "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("failed to decode base64: %v", err)
	}
	return append(data, make([]byte, 105*1024)...)
}
