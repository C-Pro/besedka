package auth

import (
	"context"
	"encoding/base64"
	"testing"
	"time"
)

func TestAuthService(t *testing.T) {
	// Test Vectors generated using github.com/pquerna/otp
	// RawSecret: 12345678901234567890
	// T0: 1700000000
	const (
		rawSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
		t0Unix    = 1700000000
	)

	// Valid codes for T0, T0+30s, T0+60s
	validCodes := []int{921300, 732303, 136087}

	// Helper to create service with fixed time
	createService := func(t *testing.T) (*AuthService, *time.Time) {
		cfg := Config{
			Secret:      base64.StdEncoding.EncodeToString([]byte("server-secret")),
			TokenExpiry: time.Hour,
		}

		ctx := context.Background()
		svc, err := NewAuthService(ctx, cfg)
		if err != nil {
			t.Fatalf("Failed to create service: %v", err)
		}

		// Mock time
		currentTime := time.Unix(t0Unix, 0)
		svc.now = func() time.Time {
			return currentTime
		}

		return svc, &currentTime
	}

	t.Run("AddUser", func(t *testing.T) {
		svc, _ := createService(t)

		u1, err := svc.AddUser("user1", "pass1")
		if err != nil {
			t.Fatalf("Failed to add user: %v", err)
		}
		if u1.Username != "user1" {
			t.Errorf("Expected username user1, got %s", u1.Username)
		}

		_, err = svc.AddUser("user1", "pass2")
		if err != ErrUserExists {
			t.Errorf("Expected ErrUserExists, got %v", err)
		}
	})

	t.Run("Login_FirstTime", func(t *testing.T) {
		svc, _ := createService(t)
		if _, err := svc.AddUser("user1", "pass1"); err != nil {
			t.Fatalf("failed to setup user: %v", err)
		}

		// First login - should require registration/setup
		resp, _ := svc.Login(LoginRequest{
			Username: "user1",
			Password: "pass1",
			TOTP:     0,
		})

		if !resp.NeedRegister {
			t.Error("Expected NeedRegister=true for first login")
		}
		if resp.Success {
			t.Error("Expected Success=false for first login")
		}
	})

	t.Run("Login_Success", func(t *testing.T) {
		svc, now := createService(t)

		// Manually setup user with TOTP secret to simulate registered user
		tx := svc.users.Lock()
		userID := "user-id-1"
		tx.Set("user1", &UserCredentials{
			UserID:       userID,
			Username:     "user1",
			PasswordHash: svc.hashPassword("user1", "pass1"),
			TOTPSecret:   rawSecret,
			LastTOTP:     0, // Initialized
		})
		tx.Unlock()

		// Attempt login with valid code
		resp, token := svc.Login(LoginRequest{
			Username: "user1",
			Password: "pass1",
			TOTP:     validCodes[0],
		})

		if !resp.Success {
			t.Errorf("Login failed: %s", resp.Message)
		}
		if token != userID {
			t.Errorf("Expected token user ID %s, got %s", userID, token)
		}

		// Verify token is live
		val, err := svc.liveTokens.Get(resp.Token)
		if err != nil || val != userID {
			t.Errorf("Token not found in liveTokens")
		}

		// Advance time and try next code
		*now = now.Add(30 * time.Second)
		resp, _ = svc.Login(LoginRequest{
			Username: "user1",
			Password: "pass1",
			TOTP:     validCodes[1],
		})
		if !resp.Success {
			t.Errorf("Login failed with second code: %s", resp.Message)
		}
	})

	t.Run("Login_Failures", func(t *testing.T) {
		svc, _ := createService(t)

		// Setup user
		tx := svc.users.Lock()
		tx.Set("user1", &UserCredentials{
			UserID:       "uid",
			Username:     "user1",
			PasswordHash: svc.hashPassword("user1", "pass1"),
			TOTPSecret:   rawSecret,
			LastTOTP:     0,
		})
		tx.Unlock()

		tests := []struct {
			name    string
			req     LoginRequest
			wantMsg string
		}{
			{
				name: "Wrong Password",
				req: LoginRequest{
					Username: "user1",
					Password: "wrongpass",
					TOTP:     validCodes[0],
				},
				wantMsg: loginFailedMessage,
			},
			{
				name: "Wrong TOTP",
				req: LoginRequest{
					Username: "user1",
					Password: "pass1",
					TOTP:     123456,
				},
				wantMsg: loginFailedMessage,
			},
			{
				name: "User Not Found",
				req: LoginRequest{
					Username: "unknown",
					Password: "pass1",
					TOTP:     validCodes[0],
				},
				wantMsg: loginFailedMessage,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				resp, _ := svc.Login(tt.req)
				if resp.Success {
					t.Error("Expected login failure")
				}
				if resp.Message != tt.wantMsg {
					t.Errorf("Expected message %q, got %q", tt.wantMsg, resp.Message)
				}
			})
		}
	})

	t.Run("Security_ReplayAttack", func(t *testing.T) {
		svc, _ := createService(t)

		// Setup user
		tx := svc.users.Lock()
		tx.Set("user1", &UserCredentials{
			UserID:       "uid",
			Username:     "user1",
			PasswordHash: svc.hashPassword("user1", "pass1"),
			TOTPSecret:   rawSecret,
			LastTOTP:     0,
		})
		tx.Unlock()

		// First login success
		resp, _ := svc.Login(LoginRequest{
			Username: "user1",
			Password: "pass1",
			TOTP:     validCodes[0],
		})
		if !resp.Success {
			t.Fatalf("First login failed")
		}

		// Replay same code
		resp, _ = svc.Login(LoginRequest{
			Username: "user1",
			Password: "pass1",
			TOTP:     validCodes[0],
		})
		if resp.Success {
			t.Error("Replay attack succeeded")
		}
	})

	t.Run("Security_Throttling", func(t *testing.T) {
		svc, now := createService(t)

		// Setup user
		tx := svc.users.Lock()
		tx.Set("user1", &UserCredentials{
			UserID:       "uid",
			Username:     "user1",
			PasswordHash: svc.hashPassword("user1", "pass1"),
			TOTPSecret:   rawSecret,
			LastTOTP:     0,
		})
		tx.Unlock()

		// Fail 4 times (threshold is > 3)
		for i := 0; i < 4; i++ {
			svc.Login(LoginRequest{
				Username: "user1",
				Password: "wrongpass",
				TOTP:     validCodes[0],
			})
		}

		// 5th attempt should be throttled
		resp, _ := svc.Login(LoginRequest{
			Username: "user1",
			Password: "pass1",
			TOTP:     validCodes[0],
		})

		if resp.Success {
			t.Error("Throttling failed, login succeeded")
		}
		// Check for throttling message
		// "Too many failed login attempts"
		if len(resp.Message) < 20 {
			t.Errorf("Expected throttling message, got %q", resp.Message)
		}

		// Advance time past backoff
		// Backoff = 30 * (failedAttempts^2)
		// failedAttempts = 4 -> 30 * 16 = 480 seconds
		*now = now.Add(500 * time.Second)

		// Should succeed now
		// Note: TOTP code validity depends on time.
		// T0 + 500s is way past validCodes[0] window.
		// We need to generate a valid code for the new time, OR just check that we get past the throttling check.
		// If we use a wrong password, we should get "Login failed" instead of "Too many..."

		resp, _ = svc.Login(LoginRequest{
			Username: "user1",
			Password: "wrongpass", // Still wrong, but check message
			TOTP:     validCodes[0],
		})

		if resp.Message != loginFailedMessage {
			t.Errorf("Expected standard failure message after backoff, got %q", resp.Message)
		}
	})

	t.Run("Logoff", func(t *testing.T) {
		svc, _ := createService(t)

		// Setup user
		tx := svc.users.Lock()
		tx.Set("user1", &UserCredentials{
			UserID:       "uid",
			Username:     "user1",
			PasswordHash: svc.hashPassword("user1", "pass1"),
			TOTPSecret:   rawSecret,
			LastTOTP:     0,
		})
		tx.Unlock()

		// Login
		resp, _ := svc.Login(LoginRequest{
			Username: "user1",
			Password: "pass1",
			TOTP:     validCodes[0],
		})
		if !resp.Success {
			t.Fatalf("Login failed")
		}

		// Verify token exists
		_, err := svc.liveTokens.Get(resp.Token)
		if err != nil {
			t.Fatalf("Token should be valid")
		}

		// Logoff
		if err := svc.Logoff(resp.Token); err != nil {
			t.Errorf("Logoff failed: %v", err)
		}

		// Verify token is gone
		_, err = svc.liveTokens.Get(resp.Token)
		if err == nil {
			t.Error("Token should be invalid after logoff")
		}
	})

	t.Run("Register", func(t *testing.T) {
		svc, now := createService(t)
		_, err := svc.AddUser("user1", "pass1")
		if err != nil {
			t.Fatalf("Failed to add user: %v", err)
		}

		// Register with correct old password
		resp := svc.Register(RegistrationRequest{
			Username:    "user1",
			Password:    "pass1",
			NewPassword: "pass2",
		})

		if !resp.Success {
			t.Fatalf("Registration failed: %s", resp.Message)
		}
		if resp.TOTPSecret == "" {
			t.Error("Expected TOTP secret in registration response")
		}

		// Try logging in with OLD password - should fail
		loginResp, _ := svc.Login(LoginRequest{
			Username: "user1",
			Password: "pass1",
			TOTP:     0,
		})
		if loginResp.Success {
			t.Error("Login with old password should fail")
		}

		// Generate valid code for new secret
		code, err := GenerateTOTP(resp.TOTPSecret, *now)
		if err != nil {
			t.Fatalf("Failed to generate TOTP: %v", err)
		}

		// Login with NEW password and TOTP
		loginResp, _ = svc.Login(LoginRequest{
			Username: "user1",
			Password: "pass2",
			TOTP:     code,
		})

		if !loginResp.Success {
			t.Errorf("Login with new password failed: %s", loginResp.Message)
		}
	})
}
