package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"besedka/internal/models"

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

type storage interface {
	UpsertCredentials(credentials UserCredentials) error
	ListCredentials() ([]UserCredentials, error)
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTP     int    `json:"totp"`
}

type RegistrationRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	NewPassword string `json:"newPassword"`
}

type RegistrationResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message,omitempty"`
	TOTPSecret string `json:"totpSecret,omitempty"`
}

type LoginResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message,omitempty"`
	NeedRegister bool   `json:"needRegister,omitempty"`
	Token        string `json:"token,omitempty"`
	TokenExpiry  int64  `json:"tokenExpiry,omitempty"`
}

type UserCredentials struct {
	models.User

	PasswordHash string `json:"passwordHash"`
	TOTPSecret   string `json:"totpSecret"`
	// Remember last TOTP to prevent replay attacks
	LastTOTP int `json:"lastTOTP"`
	// Counter for consecutive failed login attempts to throttle brute force attacks.
	FailedLoginAttempts int64 `json:"failedLoginAttempts"`
	LastAttemptTime     int64 `json:"lastAttemptTime"`
}

func (uc *UserCredentials) ResetFailedLoginAttempts(now time.Time) {
	uc.FailedLoginAttempts = 0
	uc.LastAttemptTime = now.Unix()
}

func (uc *UserCredentials) IncrementFailedLoginAttempts(now time.Time) {
	uc.FailedLoginAttempts++
	uc.LastAttemptTime = now.Unix()
}

type Config struct {
	Secret      string        `json:"secret"`
	secretBytes []byte        `json:"-"`
	TokenExpiry time.Duration `json:"tokenExpiry"`
}

type AuthService struct {
	Config
	storage storage
	// Map of userID to user credentials
	users *geche.Locker[string, *UserCredentials]
	// Map of username to userID
	usernames geche.Geche[string, string]
	// Map of token to user ID
	liveTokens geche.Geche[string, string]
	now        func() time.Time
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

func NewAuthService(ctx context.Context, config Config, storage storage) (*AuthService, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	as := &AuthService{
		Config:     config,
		storage:    storage,
		users:      geche.NewLocker(geche.NewMapCache[string, *UserCredentials]()),
		usernames:  geche.NewMapCache[string, string](),
		liveTokens: geche.NewMapTTLCache[string, string](ctx, config.TokenExpiry, time.Minute),
		now:        time.Now,
	}

	// Load users from storage
	creds, err := storage.ListCredentials()
	if err != nil {
		return nil, fmt.Errorf("failed to list credentials: %w", err)
	}

	tx := as.users.Lock()
	defer tx.Unlock()

	for _, c := range creds {
		tx.Set(c.ID, &c)
		as.usernames.Set(c.UserName, c.ID)
	}

	return as, nil
}

func (as *AuthService) hashPassword(username, password string) string {
	h := hmac.New(sha512.New, as.secretBytes)
	h.Write([]byte(username + password))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func (as *AuthService) AddUser(username, password string) (*models.User, error) {
	return as.SeedUser(uuid.NewString(), username, password, username, "")
}

func (as *AuthService) SeedUser(userID, username, password, displayName, avatarURL string) (*models.User, error) {
	tx := as.users.Lock()
	defer tx.Unlock()
	if _, err := as.usernames.Get(username); err == nil {
		return nil, ErrUserExists
	}

	creds := UserCredentials{
		User: models.User{
			ID:          userID,
			UserName:    username,
			DisplayName: displayName,
			AvatarURL:   avatarURL,
		},
		PasswordHash: as.hashPassword(username, password),
		LastTOTP:     -1,
	}

	if err := as.storage.UpsertCredentials(creds); err != nil {
		return nil, fmt.Errorf("failed to persist user: %w", err)
	}

	tx.Set(userID, &creds)
	as.usernames.Set(username, userID)

	return &creds.User, nil
}

func (as *AuthService) GetUser(id string) (models.User, error) {
	tx := as.users.RLock()
	defer tx.Unlock()
	if creds, err := tx.Get(id); err == nil {
		return creds.User, nil
	}
	return models.User{}, errors.New("user not found")
}

func (as *AuthService) GetUsers() ([]models.User, error) {
	tx := as.users.RLock()
	defer tx.Unlock()
	snap := tx.Snapshot()
	users := make([]models.User, 0, len(snap))
	for _, creds := range snap {
		users = append(users, creds.User)
	}
	return users, nil
}

func (as *AuthService) Login(req LoginRequest) (LoginResponse, string) {
	now := as.now()
	tx := as.users.RLock()
	defer tx.Unlock()

	id, err := as.usernames.Get(req.Username)
	if err != nil {
		return LoginResponse{
			Success: false,
			Message: loginFailedMessage,
		}, ""
	}

	user, err := tx.Get(id)
	if err != nil {
		return LoginResponse{
			Success: false,
			Message: loginFailedMessage,
		}, ""
	}

	// Check failed login attempts
	if user.FailedLoginAttempts > 3 {
		lastAttempt := user.LastAttemptTime
		failedAttempts := user.FailedLoginAttempts
		nextAttempt := lastAttempt + 30*(failedAttempts*failedAttempts)
		if now.Unix() < nextAttempt {
			return LoginResponse{
				Success: false,
				Message: fmt.Sprintf("Too many failed login attempts. Next attempt in %d seconds", nextAttempt-now.Unix()),
			}, ""
		}
	}

	// Use constant-time comparison for password hashes
	currentHash := as.hashPassword(req.Username, req.Password)
	if !hmac.Equal([]byte(user.PasswordHash), []byte(currentHash)) {
		user.IncrementFailedLoginAttempts(now)
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

	// Do not allow to reuse TOTP (possible replay attack).
	if user.LastTOTP == req.TOTP {
		user.IncrementFailedLoginAttempts(now)
		return LoginResponse{
			Success: false,
			Message: loginFailedMessage,
		}, ""
	}

	if !as.checkTOTP(user.TOTPSecret, req.TOTP, user.LastTOTP) {
		user.IncrementFailedLoginAttempts(now)
		return LoginResponse{
			Success: false,
			Message: loginFailedMessage,
		}, ""
	}

	token, err := as.generateToken()
	if err != nil {
		slog.Error("login failed", "user_id", user.ID, "error", err)
		return LoginResponse{
			Success: false,
			Message: "internal error",
		}, ""
	}

	as.liveTokens.Set(token, user.ID)
	user.ResetFailedLoginAttempts(now)
	// Update LastTOTP to prevent replay attacks
	user.LastTOTP = req.TOTP

	if err := as.storage.UpsertCredentials(*user); err != nil {
		slog.Error("failed to persist user after login", "error", err)
	}

	return LoginResponse{
		Success:     true,
		Token:       token,
		TokenExpiry: now.Unix() + int64(as.TokenExpiry.Seconds()),
	}, user.ID
}

func (as *AuthService) Logoff(token string) error {
	userID, err := as.GetUserID(token)
	if err != nil {
		return nil
	}

	as.SetOffline(userID)
	_ = as.liveTokens.Del(token)

	return nil
}

func (as *AuthService) SetOffline(userID string) {
	tx := as.users.Lock()
	defer tx.Unlock()

	user, err := tx.Get(userID)
	if err != nil {
		return
	}

	user.Presence = models.Presence{
		Online:   false,
		LastSeen: as.now().Unix(),
	}

	if err := as.storage.UpsertCredentials(*user); err != nil {
		slog.Error("failed to set user offline in storage", "user_id", userID, "error", err)
	}
	tx.Set(userID, user)
}

func (as *AuthService) SetOnline(userID string) {
	tx := as.users.Lock()
	defer tx.Unlock()

	user, err := tx.Get(userID)
	if err != nil {
		return
	}

	user.Presence = models.Presence{
		Online:   true,
		LastSeen: as.now().Unix(),
	}

	if err := as.storage.UpsertCredentials(*user); err != nil {
		slog.Error("failed to set user online in storage", "user_id", userID, "error", err)
	}
	tx.Set(userID, user)
}

func (as *AuthService) generateToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func (as *AuthService) generateTOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate secret: %w", err)
	}
	return base32.StdEncoding.EncodeToString(b), nil
}

func (as *AuthService) Register(req RegistrationRequest) RegistrationResponse {
	tx := as.users.Lock()
	defer tx.Unlock()

	id, err := as.usernames.Get(req.Username)
	if err != nil {
		return RegistrationResponse{
			Success: false,
			Message: "User not found",
		}
	}

	user, err := tx.Get(id)
	if err != nil {
		return RegistrationResponse{
			Success: false,
			Message: "User not found",
		}
	}

	if user.LastTOTP != -1 {
		return RegistrationResponse{
			Success: false,
			Message: "User already registered",
		}
	}

	hash := as.hashPassword(req.Username, req.Password)
	if !hmac.Equal([]byte(user.PasswordHash), []byte(hash)) {
		return RegistrationResponse{
			Success: false,
			Message: "Invalid password",
		}
	}

	secret, err := as.generateTOTPSecret()
	if err != nil {
		return RegistrationResponse{
			Success: false,
			Message: "Internal error",
		}
	}
	user.PasswordHash = as.hashPassword(req.Username, req.NewPassword)
	user.TOTPSecret = secret
	user.LastTOTP = 0 // Activate user

	if err := as.storage.UpsertCredentials(*user); err != nil {
		slog.Error("failed to persist user after registration", "error", err)
		return RegistrationResponse{
			Success: false,
			Message: "Internal error",
		}
	}

	tx.Set(req.Username, user)

	return RegistrationResponse{
		Success:    true,
		TOTPSecret: secret,
	}
}

func GenerateTOTP(secret string, t time.Time) (int, error) {
	key, err := base32.StdEncoding.DecodeString(secret)
	if err != nil {
		return 0, fmt.Errorf("invalid base32 secret: %w", err)
	}

	buf := make([]byte, 8)
	counter := t.Unix() / 30
	h := hmac.New(sha1.New, key)
	binary.BigEndian.PutUint64(buf, uint64(counter))
	h.Write(buf)
	sum := h.Sum(nil)

	off := sum[len(sum)-1] & 0xf
	trunc := (int(sum[off])&0x7f)<<24 |
		int(sum[off+1])<<16 |
		int(sum[off+2])<<8 |
		int(sum[off+3])

	return trunc % 1e6, nil
}

func (as *AuthService) checkTOTP(secret string, totp int, lastTOTP int) bool {
	if totp == lastTOTP {
		return false
	}

	// Check current, prev, next windows to allow for clock skew
	for i := -1; i <= 1; i++ {
		t := as.now().Add(time.Duration(i*30) * time.Second)
		code, err := GenerateTOTP(secret, t)
		if err != nil {
			continue
		}

		if totp == code {
			return true
		}
	}
	return false
}

func (as *AuthService) GetUserID(token string) (string, error) {
	userID, err := as.liveTokens.Get(token)
	if err != nil {
		return "", err
	}

	tx := as.users.Lock()
	defer tx.Unlock()

	user, err := tx.Get(userID)
	if err != nil {
		return "", err
	}

	user.Presence = models.Presence{
		Online:   true,
		LastSeen: as.now().Unix(),
	}

	// Update token expiry, so while user is active at least once per TokenExpiry interval,
	// token will be extended indefinitely without requiring user to relogin.
	as.liveTokens.Set(token, userID)

	return user.ID, nil
}
