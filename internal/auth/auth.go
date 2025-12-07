package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/c-pro/geche"
	"github.com/google/uuid"
)

const (
	DefaultTokenExpiry = 12 * time.Hour
	loginFailedMessage = "Login failed"
)

var (
	ErrUserExists = errors.New("user already exists")
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTP     int    `json:"totp"`
}

// RegistrationRequest is sent by a user to finalize
// the registration after their first login
// with a one-time password.
type RegistrationRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totpSecret"`
}

type LoginResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message,omitempty"`
	NeedRegister bool   `json:"needRegister,omitempty"`
	Token        string `json:"token,omitempty"`
	TokenExpiry  int64  `json:"tokenExpiry,omitempty"`
}

type UserCredentials struct {
	UserID       string `json:"userId"`
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
	TOTPSecret   string `json:"totpSecret"`
	// Remember last TOTP to prevent replay attacks
	LastTOTP int `json:"lastTOTP"`
	// CounterForConsecutive failed login attempts to throttle brute force attacks.
	FailedLoginAttempts int64 `json:"failedLoginAttempts"`
	LastAttemptTime     int64 `json:"lastAttemptTime"`
}

func (uc *UserCredentials) ResetFailedLoginAttempts() {
	atomic.StoreInt64(&uc.FailedLoginAttempts, 0)
	atomic.StoreInt64(&uc.LastAttemptTime, time.Now().Unix())
}

func (uc *UserCredentials) IncrementFailedLoginAttempts() {
	atomic.AddInt64(&uc.FailedLoginAttempts, 1)
	atomic.StoreInt64(&uc.LastAttemptTime, time.Now().Unix())
}

type Config struct {
	Secret      string        `json:"secret"`
	secretBytes []byte        `json:"-"`
	TokenExpiry time.Duration `json:"tokenExpiry"`
}

type AuthService struct {
	Config
	users      geche.Geche[string, *UserCredentials]
	liveTokens geche.Geche[string, string]
}

func (c *Config) Validate() error {
	if c.Secret == "" {
		return errors.New("secret is required")
	}

	var err error
	c.secretBytes, err = base64.StdEncoding.DecodeString(c.Secret)
	if err != nil {
		return fmt.Errorf("auth secret is not a valid base64: %w", err)
	}

	if c.TokenExpiry == 0 {
		c.TokenExpiry = DefaultTokenExpiry
	}

	return nil
}

func NewAuthService(ctx context.Context, config Config) (*AuthService, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &AuthService{
		Config:     config,
		users:      geche.NewMapCache[string, *UserCredentials](),
		liveTokens: geche.NewMapTTLCache[string, string](ctx, config.TokenExpiry, time.Minute),
	}, nil
}

func (as *AuthService) hashPassword(username, password string) string {
	h := hmac.New(sha512.New, as.secretBytes)
	h.Write([]byte(username + password))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func (as *AuthService) AddUser(username, password string) (UserCredentials, error) {
	if _, err := as.users.Get(username); err == nil {
		return UserCredentials{}, ErrUserExists
	}

	userID := uuid.NewString()
	passwordHash := as.hashPassword(username, password)
	as.users.Set(username,
		&UserCredentials{
			UserID:       userID,
			Username:     username,
			PasswordHash: passwordHash,
		})

	return UserCredentials{
		UserID:   userID,
		Username: username,
	}, nil
}

func (as *AuthService) Login(req LoginRequest) (LoginResponse, string) {
	user, err := as.users.Get(req.Username)
	if err != nil {
		return LoginResponse{
			Success: false,
			Message: loginFailedMessage,
		}, ""
	}

	if user.FailedLoginAttempts > 3 {
		lastAttempt := atomic.LoadInt64(&user.LastAttemptTime)
		nextAttempt := lastAttempt + 30*(user.FailedLoginAttempts*user.FailedLoginAttempts)
		if time.Now().Unix() < nextAttempt {
			return LoginResponse{
				Success: false,
				Message: fmt.Sprintf("Too many failed login attempts. Next attempt in %d seconds", nextAttempt-time.Now().Unix()),
			}, ""
		}
	}

	if user.PasswordHash != as.hashPassword(req.Username, req.Password) {
		user.IncrementFailedLoginAttempts()
		return LoginResponse{
			Success: false,
			Message: loginFailedMessage,
		}, ""
	}

	if user.LastTOTP == -1 {
		return LoginResponse{
			NeedRegister: true,
			Message:      "First login requires to change password and setup TOTP",
		}, ""
	}

	if user.LastTOTP == req.TOTP {
		user.IncrementFailedLoginAttempts()
		return LoginResponse{
			Success: false,
			Message: loginFailedMessage,
		}, ""
	}

	if !as.checkTOTP(user.TOTPSecret, req.TOTP, user.LastTOTP) {
		user.IncrementFailedLoginAttempts()
		return LoginResponse{
			Success: false,
			Message: loginFailedMessage,
		}, ""
	}

	token, err := as.generateToken()
	if err != nil {
		slog.Error("login failed", "user_id", user.UserID, "error", err)
		return LoginResponse{
			Success: false,
			Message: "internal error",
		}, ""
	}

	as.liveTokens.Set(token, user.UserID)
	user.ResetFailedLoginAttempts()

	return LoginResponse{
		Success:     true,
		Token:       token,
		TokenExpiry: time.Now().Unix() + int64(as.TokenExpiry.Seconds()),
	}, user.UserID
}

func (as *AuthService) generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func (as *AuthService) checkTOTP(secret string, totp int, lastTOTP int) bool {
	if totp == lastTOTP {
		return false
	}
	buf := make([]byte, 8)
	for i := -1; i <= 1; i++ {
		t := (time.Now().Unix() + int64(i*30)) / 30
		h := hmac.New(sha1.New, []byte(secret))
		binary.BigEndian.PutUint64(buf, uint64(t))
		h.Write(buf)
		sum := h.Sum(nil)

		off := sum[len(sum)-1] & 0xf
		trunc := (int(sum[off])&0x7f)<<24 |
			int(sum[off+1])<<16 |
			int(sum[off+2])<<8 |
			int(sum[off+3])

		if totp == trunc%1e6 {
			return true
		}
	}
	return false
}
