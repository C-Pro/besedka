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
	"slices"
	"time"

	"besedka/internal/content"
	"besedka/internal/models"

	"github.com/c-pro/geche"
	"github.com/google/uuid"
)

const (
	DefaultTokenExpiry             = 24 * time.Hour
	DefaultRegistrationTokenExpiry = 24 * time.Hour
	loginFailedMessage             = "Login failed"
)

var (
	ErrUserExists = errors.New("user already exists")
)

type storage interface {
	UpsertCredentials(credentials UserCredentials) error
	ListCredentials() ([]UserCredentials, error)
	ListAllCredentials() ([]UserCredentials, error)
	// UpsertToken saves a token for a user. The token should be hashed.
	UpsertToken(userID string, tokenHash string) error
	// DeleteToken deletes a specific token (by hash).
	DeleteToken(tokenHash string) error
	// ListTokens returns all tokens (hashed) and their associated user IDs.
	ListTokens() (map[string]string, error)
	UpsertRegistrationToken(userID string, token string) error
	DeleteRegistrationToken(userID string) error
	ListRegistrationTokens() (map[string]string, error)
	MigrateTokens(hasher func(string) string) error
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	TOTP     int    `json:"totp"`
}

type RegistrationRequest struct {
	Token       string `json:"token"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
	TOTP        int    `json:"totp"`
}

type RegistrationResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token,omitempty"`
	Message string `json:"message,omitempty"`
}

type RegistrationInfoResponse struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	TOTPSecret  string `json:"totpSecret"`
}

type LoginResponse struct {
	Success     bool   `json:"success"`
	Message     string `json:"message,omitempty"`
	Token       string `json:"token,omitempty"`
	TokenExpiry int64  `json:"tokenExpiry,omitempty"`
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
	Secret                  string        `json:"secret"`
	secretBytes             []byte        `json:"-"`
	TokenExpiry             time.Duration `json:"tokenExpiry"`
	RegistrationTokenExpiry time.Duration `json:"registrationTokenExpiry"`
}

type AuthService struct {
	Config
	storage storage
	// Map of userID to user credentials
	users *geche.Locker[string, *UserCredentials]
	// Map of username to userID
	usernames geche.Geche[string, string]
	// Map of token to user ID
	liveTokens *geche.MapTTLCache[string, string]
	// Index of all tokens per user
	userTokens *geche.Locker[string, []string]
	// Map of registration token to user ID
	registrationTokens *geche.MapTTLCache[string, string]
	now                func() time.Time
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
	if c.RegistrationTokenExpiry == 0 {
		c.RegistrationTokenExpiry = DefaultRegistrationTokenExpiry
	}

	return nil
}

func NewAuthService(ctx context.Context, config Config, storage storage) (*AuthService, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	as := &AuthService{
		Config:             config,
		storage:            storage,
		users:              geche.NewLocker(geche.NewMapCache[string, *UserCredentials]()),
		usernames:          geche.NewMapCache[string, string](),
		liveTokens:         geche.NewMapTTLCache[string, string](ctx, config.TokenExpiry, time.Minute),
		userTokens:         geche.NewLocker(geche.NewMapCache[string, []string]()),
		registrationTokens: geche.NewMapTTLCache[string, string](ctx, config.RegistrationTokenExpiry, time.Minute),
		now:                time.Now,
	}

	if err := storage.MigrateTokens(func(token string) string {
		return as.hashToken(token)
	}); err != nil {
		return nil, fmt.Errorf("failed to migrate tokens: %w", err)
	}

	// Load users from storage
	creds, err := storage.ListAllCredentials()
	if err != nil {
		return nil, fmt.Errorf("failed to list credentials: %w", err)
	}

	tx := as.users.Lock()
	defer tx.Unlock()

	for _, c := range creds {
		// Reset online status on storage load
		c.Presence.Online = false
		tx.Set(c.ID, &c)
		as.usernames.Set(c.UserName, c.ID)
	}

	// Load tokens from storage
	tokens, err := storage.ListTokens()
	if err != nil {
		return nil, fmt.Errorf("failed to list tokens: %w", err)
	}
	userTokensTx := as.userTokens.Lock()
	defer userTokensTx.Unlock()

	for tokenHash, userID := range tokens {
		as.liveTokens.Set(tokenHash, userID)
		userTokens, _ := userTokensTx.Get(userID)
		userTokensTx.Set(userID, append(userTokens, tokenHash))
	}

	as.liveTokens.OnEvict(func(tokenHash string, userID string) {
		userTokensTx := as.userTokens.Lock()
		defer userTokensTx.Unlock()
		userTokens, _ := userTokensTx.Get(userID)
		userTokens = slices.DeleteFunc(userTokens, func(t string) bool {
			return t == tokenHash
		})
		userTokensTx.Set(userID, userTokens)
		if err := storage.DeleteToken(tokenHash); err != nil {
			slog.Error("failed to delete token from storage on eviction", "user_id", userID, "error", err)
		}
	})

	// Load registration tokens from storage
	regTokens, err := storage.ListRegistrationTokens()
	if err != nil {
		return nil, fmt.Errorf("failed to list registration tokens: %w", err)
	}
	for userID, token := range regTokens {
		as.registrationTokens.Set(token, userID)
	}

	as.registrationTokens.OnEvict(func(token string, userID string) {
		if err := storage.DeleteRegistrationToken(userID); err != nil {
			slog.Error("failed to delete registration token from storage on eviction", "user_id", userID, "error", err)
		}
	})

	return as, nil
}

func (as *AuthService) hashPassword(username, password string) string {
	h := hmac.New(sha512.New, as.secretBytes)
	h.Write([]byte(username + password))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

func (as *AuthService) hashToken(token string) string {
	h := hmac.New(sha512.New, as.secretBytes)
	h.Write([]byte(token))
	return string(h.Sum(nil))
}

func (as *AuthService) UpdateAvatarURL(userID string, avatarURL string) error {
	tx := as.users.Lock()
	defer tx.Unlock()

	user, err := tx.Get(userID)
	if err != nil {
		return models.ErrNotFound
	}

	user.AvatarURL = avatarURL

	if err := as.storage.UpsertCredentials(*user); err != nil {
		return fmt.Errorf("failed to persist user avatar url: %w", err)
	}

	tx.Set(user.ID, user)

	return nil
}

func (as *AuthService) UpdateDisplayName(userID string, displayName string) error {
	tx := as.users.Lock()
	defer tx.Unlock()

	user, err := tx.Get(userID)
	if err != nil {
		return models.ErrNotFound
	}

	displayName = content.Sanitize(displayName)
	if displayName == "" {
		return fmt.Errorf("display name cannot be empty")
	}

	user.DisplayName = displayName

	if err := as.storage.UpsertCredentials(*user); err != nil {
		return fmt.Errorf("failed to persist user display name: %w", err)
	}

	tx.Set(user.ID, user)

	return nil
}

func (as *AuthService) AddUser(username, displayName string) (string, error) {
	tx := as.users.Lock()
	defer tx.Unlock()

	// Validate inputs
	if err := content.ValidateUsername(username); err != nil {
		return "", fmt.Errorf("invalid username: %w", err)
	}
	username = content.Sanitize(username)
	displayName = content.Sanitize(displayName)

	var user *UserCredentials
	// If the user exists but did not finish their registration, we will generate a new registration token.
	// If registration is already complete, we will return ErrUserExists.
	if id, err := as.usernames.Get(username); err == nil {
		existingUser, err := tx.Get(id)
		if err != nil {
			return "", fmt.Errorf("user found in username index but missing in storage: %w", err)
		}
		if existingUser.LastTOTP != -1 {
			return "", ErrUserExists
		}
		user = existingUser
	} else {
		// Create new user
		userID := uuid.NewString()
		totpSecret, err := as.generateTOTPSecret()
		if err != nil {
			return "", fmt.Errorf("failed to generate TOTP secret: %w", err)
		}

		user = &UserCredentials{
			User: models.User{
				ID:          userID,
				UserName:    username,
				DisplayName: displayName,
				Status:      models.UserStatusCreated,
			},
			TOTPSecret: totpSecret,
			LastTOTP:   -1,
		}
	}

	// Generate registration token
	token, err := as.generateToken()
	if err != nil {
		return "", fmt.Errorf("failed to generate registration token: %w", err)
	}

	if err := as.storage.UpsertCredentials(*user); err != nil {
		return "", fmt.Errorf("failed to persist user: %w", err)
	}

	if err := as.storage.UpsertRegistrationToken(user.ID, token); err != nil {
		return "", fmt.Errorf("failed to persist registration token: %w", err)
	}

	tx.Set(user.ID, user)
	as.usernames.Set(username, user.ID)
	// Remove any existing registration tokens for this user from cache to invalidate old links
	for k, v := range as.registrationTokens.Snapshot() {
		if v == user.ID {
			_ = as.registrationTokens.Del(k)
		}
	}
	as.registrationTokens.Set(token, user.ID)

	return token, nil
}

func (as *AuthService) ResetPassword(userID string) (string, error) {
	tx := as.users.Lock()
	defer tx.Unlock()

	user, err := tx.Get(userID)
	if err != nil {
		return "", models.ErrNotFound
	}

	userTokensTx := as.userTokens.Lock()
	defer userTokensTx.Unlock()
	userTokens, _ := userTokensTx.Get(user.ID)
	for _, tokenHash := range userTokens {
		_ = as.liveTokens.Del(tokenHash)
		if err := as.storage.DeleteToken(tokenHash); err != nil {
			slog.Error("failed to delete token from storage on password reset", "token_hash", tokenHash, "error", err)
		}
	}
	userTokensTx.Set(user.ID, nil)

	totpSecret, err := as.generateTOTPSecret()
	if err != nil {
		return "", fmt.Errorf("failed to generate TOTP secret: %w", err)
	}

	token, err := as.generateToken()
	if err != nil {
		return "", fmt.Errorf("failed to generate registration token: %w", err)
	}

	user.TOTPSecret = totpSecret
	user.LastTOTP = -1
	user.FailedLoginAttempts = 0
	user.LastAttemptTime = 0
	user.PasswordHash = ""
	user.Status = models.UserStatusCreated

	// Set presence to offline
	user.Presence = models.Presence{
		Online:   false,
		LastSeen: as.now().Unix(),
	}

	if err := as.storage.UpsertCredentials(*user); err != nil {
		return "", fmt.Errorf("failed to persist user: %w", err)
	}

	if err := as.storage.UpsertRegistrationToken(user.ID, token); err != nil {
		return "", fmt.Errorf("failed to persist registration token: %w", err)
	}

	tx.Set(user.ID, user)

	// Remove any existing registration tokens for this user from cache to invalidate old links
	for k, v := range as.registrationTokens.Snapshot() {
		if v == user.ID {
			_ = as.registrationTokens.Del(k)
		}
	}
	as.registrationTokens.Set(token, user.ID)

	return token, nil
}

func (as *AuthService) DeleteUser(userID string) error {
	tx := as.users.Lock()
	defer tx.Unlock()

	user, err := tx.Get(userID)
	if err != nil {
		return models.ErrNotFound
	}

	user.Status = models.UserStatusDeleted
	user.PasswordHash = ""
	user.TOTPSecret = ""

	// Initializing presence to offline
	user.Presence = models.Presence{
		Online:   false,
		LastSeen: as.now().Unix(),
	}

	if err := as.storage.UpsertCredentials(*user); err != nil {
		return fmt.Errorf("failed to persist deleted user: %w", err)
	}

	userTokensTx := as.userTokens.Lock()
	defer userTokensTx.Unlock()
	userTokens, _ := userTokensTx.Get(user.ID)
	for _, tokenHash := range userTokens {
		_ = as.liveTokens.Del(tokenHash)
		if err := as.storage.DeleteToken(tokenHash); err != nil {
			slog.Error("failed to delete token from storage", "token_hash", tokenHash, "error", err)
		}
	}
	_ = userTokensTx.Del(user.ID)

	tx.Set(user.ID, user)

	return nil
}

func (as *AuthService) GetUser(id string) (models.User, error) {
	tx := as.users.RLock()
	defer tx.Unlock()
	if creds, err := tx.Get(id); err == nil {
		return creds.User, nil
	}
	return models.User{}, models.ErrNotFound
}

func (as *AuthService) GetUsers() ([]models.User, error) {
	tx := as.users.RLock()
	defer tx.Unlock()
	snap := tx.Snapshot()
	users := make([]models.User, 0, len(snap))
	for _, creds := range snap {
		if creds.Status == models.UserStatusActive {
			users = append(users, creds.User)
		}
	}
	return users, nil
}

func (as *AuthService) GetAllUsers() ([]models.User, error) {
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

	if user.Status != models.UserStatusActive {
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
			Success: false,
			Message: "User setup not completed",
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

	tokenHash := as.hashToken(token)
	as.liveTokens.Set(tokenHash, user.ID)

	// Add to userTokens
	userTokensTx := as.userTokens.Lock()
	userTokens, _ := userTokensTx.Get(user.ID)
	// Append new token hash
	userTokensTx.Set(user.ID, append(userTokens, tokenHash))
	userTokensTx.Unlock()

	user.ResetFailedLoginAttempts(now)
	// Update LastTOTP to prevent replay attacks
	user.LastTOTP = req.TOTP

	if err := as.storage.UpsertCredentials(*user); err != nil {
		slog.Error("failed to persist user after login", "error", err)
	}

	if err := as.storage.UpsertToken(user.ID, tokenHash); err != nil {
		slog.Error("failed to persist token after login", "error", err)
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

	tokenHash := as.hashToken(token)

	if err := as.storage.DeleteToken(tokenHash); err != nil {
		slog.Error("failed to delete token from storage on logoff", "error", err)
	}
	_ = as.liveTokens.Del(tokenHash)

	// Remove from userTokens
	userTokensTx := as.userTokens.Lock()
	defer userTokensTx.Unlock()
	userTokens, _ := userTokensTx.Get(userID)
	userTokens = slices.DeleteFunc(userTokens, func(t string) bool {
		return t == tokenHash
	})
	userTokensTx.Set(userID, userTokens)

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

func (as *AuthService) GetRegistrationInfo(token string) (RegistrationInfoResponse, error) {
	userID, err := as.registrationTokens.Get(token)
	if err != nil {
		return RegistrationInfoResponse{}, errors.New("invalid or expired registration token")
	}

	tx := as.users.RLock()
	defer tx.Unlock()
	user, err := tx.Get(userID)
	if err != nil {
		return RegistrationInfoResponse{}, errors.New("user not found")
	}

	return RegistrationInfoResponse{
		Username:    user.UserName,
		DisplayName: user.DisplayName,
		TOTPSecret:  user.TOTPSecret,
	}, nil
}

func (as *AuthService) CompleteRegistration(req RegistrationRequest) (RegistrationResponse, string) {
	userID, err := as.registrationTokens.Get(req.Token)
	if err != nil {
		return RegistrationResponse{
			Success: false,
			Message: "Invalid or expired registration token",
		}, ""
	}

	tx := as.users.Lock()
	defer tx.Unlock()

	user, err := tx.Get(userID)
	if err != nil {
		return RegistrationResponse{
			Success: false,
			Message: "User not found",
		}, ""
	}

	if user.LastTOTP != -1 {
		return RegistrationResponse{
			Success: false,
			Message: "User already registered",
		}, ""
	}

	if !as.checkTOTP(user.TOTPSecret, req.TOTP, user.LastTOTP) {
		return RegistrationResponse{
			Success: false,
			Message: "Invalid TOTP code",
		}, ""
	}

	if req.DisplayName != "" {
		req.DisplayName = content.Sanitize(req.DisplayName)
	}

	user.PasswordHash = as.hashPassword(user.UserName, req.Password)
	if req.DisplayName != "" {
		user.DisplayName = req.DisplayName
	}
	user.LastTOTP = 0 // Activate user
	user.Status = models.UserStatusActive

	if err := as.storage.UpsertCredentials(*user); err != nil {
		slog.Error("failed to persist user after registration", "error", err)
		return RegistrationResponse{
			Success: false,
			Message: "Internal error",
		}, ""
	}

	// Delete registration token
	if err := as.storage.DeleteRegistrationToken(userID); err != nil {
		slog.Error("failed to delete registration token", "error", err)
	}
	_ = as.registrationTokens.Del(req.Token)

	// Create session token
	token, err := as.generateToken()
	if err != nil {
		slog.Error("login failed", "user_id", user.ID, "error", err)
		return RegistrationResponse{
			Success: false,
			Message: "Internal error",
		}, ""
	}

	tokenHash := as.hashToken(token)
	as.liveTokens.Set(tokenHash, user.ID)

	// Add to userTokens
	userTokensTx := as.userTokens.Lock()
	userTokens, _ := userTokensTx.Get(user.ID)
	userTokensTx.Set(user.ID, append(userTokens, tokenHash))
	userTokensTx.Unlock()

	if err := as.storage.UpsertToken(user.ID, tokenHash); err != nil {
		slog.Error("failed to persist token after registration", "error", err)
	}

	return RegistrationResponse{
		Success: true,
		Token:   token,
	}, token
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
	tokenHash := as.hashToken(token)
	userID, err := as.liveTokens.Get(tokenHash)
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
	as.liveTokens.Set(tokenHash, userID)

	return user.ID, nil
}
