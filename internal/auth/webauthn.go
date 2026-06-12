package auth

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"besedka/internal/models"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

type webAuthnUser struct {
	authService *AuthService
	user        *UserCredentials
}

func (w *webAuthnUser) WebAuthnID() []byte { return []byte(w.user.ID) }
func (w *webAuthnUser) WebAuthnName() string { return w.user.UserName }
func (w *webAuthnUser) WebAuthnDisplayName() string { return w.user.DisplayName }
func (w *webAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	passkeys, err := w.authService.storage.ListPasskeys(w.user.ID)
	if err != nil {
		slog.Error("failed to list passkeys in WebAuthnCredentials", "error", err, "userID", w.user.ID)
	}
	var creds []webauthn.Credential
	for _, p := range passkeys {
		var transports []protocol.AuthenticatorTransport
		for _, t := range p.Transport {
			transports = append(transports, protocol.AuthenticatorTransport(t))
		}
		creds = append(creds, webauthn.Credential{
			ID:              p.ID,
			PublicKey:       p.PublicKey,
			AttestationType: p.AttestationType,
			Transport:       transports,
			Authenticator: webauthn.Authenticator{
				AAGUID:       p.AAGUID,
				SignCount:    p.SignCount,
				CloneWarning: false,
			},
		})
	}
	return creds
}

func (a *AuthService) getWebAuthnUser(userID string) (*webAuthnUser, error) {
	tx := a.users.Lock()
	defer tx.Unlock()
	u, err := tx.Get(userID)
	if err != nil {
		return nil, models.ErrNotFound
	}
	return &webAuthnUser{authService: a, user: u}, nil
}

func (a *AuthService) BeginPasskeyRegistration(userID string) (*protocol.CredentialCreation, *webauthn.SessionData, error) {
	u, err := a.getWebAuthnUser(userID)
	if err != nil {
		return nil, nil, err
	}
	return a.webAuthn.BeginRegistration(u, webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired))
}

func (a *AuthService) FinishPasskeyRegistration(userID string, name string, sessionData *webauthn.SessionData, r *http.Request) error {
	u, err := a.getWebAuthnUser(userID)
	if err != nil {
		return err
	}
	cred, err := a.webAuthn.FinishRegistration(u, *sessionData, r)
	if err != nil {
		return err
	}
	if name == "" {
		name = "Passkey"
	}
	var transports []string
	for _, t := range cred.Transport {
		transports = append(transports, string(t))
	}
	pk := Passkey{
		ID:              cred.ID,
		UserID:          userID,
		PublicKey:       cred.PublicKey,
		AttestationType: cred.AttestationType,
		AAGUID:          cred.Authenticator.AAGUID,
		SignCount:       cred.Authenticator.SignCount,
		Name:            name,
		CreatedAt:       time.Now().Unix(),
		Transport:       transports,
		BackupEligible:  cred.Flags.BackupEligible,
		BackupState:     cred.Flags.BackupState,
	}
	return a.storage.UpsertPasskey(pk)
}

func (a *AuthService) BeginPasskeyLogin() (*protocol.CredentialAssertion, *webauthn.SessionData, error) {
	return a.webAuthn.BeginDiscoverableLogin()
}

func (a *AuthService) FinishPasskeyLogin(sessionData *webauthn.SessionData, r *http.Request) (LoginResponse, *models.User, error) {
	var userID string
	var u *webAuthnUser

	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		userID = string(userHandle)
		if userID == "" {
			return nil, errors.New("user not found")
		}
		var err error
		u, err = a.getWebAuthnUser(userID)
		return u, err
	}

	cred, err := a.webAuthn.FinishDiscoverableLogin(handler, *sessionData, r)
	if err != nil {
		return LoginResponse{Success: false, Message: "WebAuthn login failed"}, nil, err
	}

	// Update sign count
	passkeys, err := a.storage.ListPasskeys(userID)
	if err != nil {
		slog.Error("failed to list passkeys for sign count update", "error", err, "userID", userID)
	} else {
		for _, pk := range passkeys {
			if bytes.Equal(pk.ID, cred.ID) {
				pk.SignCount = cred.Authenticator.SignCount
				if err := a.storage.UpsertPasskey(pk); err != nil {
					slog.Error("failed to update passkey sign count", "error", err, "userID", userID)
				}
				break
			}
		}
	}

	token, err := a.generateToken()
	if err != nil {
		return LoginResponse{Success: false, Message: "Failed to generate token"}, nil, err
	}
	tokenHash := a.hashToken(token)

	now := a.now()
	a.liveTokens.Set(tokenHash, tokenSession{UserID: userID, UpdatedAt: now})

	userTokensTx := a.userTokens.Lock()
	userTokens, _ := userTokensTx.Get(userID)
	userTokensTx.Set(userID, append(userTokens, tokenHash))
	userTokensTx.Unlock()

	u.user.ResetFailedLoginAttempts(now)
	if err := a.storage.UpsertCredentials(*u.user); err != nil {
		return LoginResponse{Success: false, Message: "Database error"}, nil, err
	}

	if err := a.storage.UpsertToken(userID, tokenHash); err != nil {
		slog.Error("failed to persist token after passkey login", "error", err)
	}

	return LoginResponse{
		Success:      true,
		Token:        token,
		TokenExpiry:  now.Unix() + int64(a.TokenExpiry.Seconds()),
		SessionLimit: int64(a.TokenExpiry.Seconds()),
		UserID:       userID,
	}, &u.user.User, nil
}

func (a *AuthService) SaveWebAuthnSession(sessionID string, sessionData *webauthn.SessionData) {
	a.webAuthnSessions.Set(sessionID, sessionData)
}

func (a *AuthService) GetWebAuthnSession(sessionID string) (*webauthn.SessionData, bool) {
	data, err := a.webAuthnSessions.Get(sessionID)
	if err != nil {
		return nil, false
	}
	return data, true
}

func (a *AuthService) DeleteWebAuthnSession(sessionID string) {
	_ = a.webAuthnSessions.Del(sessionID)
}

func (a *AuthService) ListPasskeys(userID string) ([]Passkey, error) {
	return a.storage.ListPasskeys(userID)
}

func (a *AuthService) DeletePasskey(userID string, credentialID []byte) error {
	return a.storage.DeletePasskey(userID, credentialID)
}
