package api

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/google/uuid"
)

func generateSessionID() string {
	return uuid.New().String()
}

func (a *API) WebAuthnRegisterBeginHandler(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())

	options, sessionData, err := a.auth.BeginPasskeyRegistration(userID)
	if err != nil {
		slog.Error("Failed to begin passkey registration", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sessionID := generateSessionID() // wait, I don't have this func here. I can just use a random string. I'll define it.
	a.auth.SaveWebAuthnSession(sessionID, sessionData)

	// Set session ID in a cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "webauthn_session",
		Value:    sessionID,
		Path:     "/api/webauthn/",
		HttpOnly: true,
		Secure:   true, // Assuming HTTPS, but ok for localhost if browser allows or we don't set Secure on localhost. I'll omit Secure for dev or just rely on SameSite
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(5 * time.Minute),
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(options)
}

func (a *API) WebAuthnRegisterFinishHandler(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())

	name := r.URL.Query().Get("name")

	cookie, err := r.Cookie("webauthn_session")
	if err != nil {
		http.Error(w, "Session cookie missing", http.StatusBadRequest)
		return
	}

	sessionData, ok := a.auth.GetWebAuthnSession(cookie.Value)
	if !ok {
		http.Error(w, "Session expired or not found", http.StatusBadRequest)
		return
	}
	defer a.auth.DeleteWebAuthnSession(cookie.Value)

	err = a.auth.FinishPasskeyRegistration(userID, name, sessionData, r)
	if err != nil {
		slog.Error("Failed to finish passkey registration", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (a *API) WebAuthnLoginBeginHandler(w http.ResponseWriter, r *http.Request) {
	options, sessionData, err := a.auth.BeginPasskeyLogin()
	if err != nil {
		slog.Error("Failed to begin passkey login", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sessionID := generateSessionID()
	a.auth.SaveWebAuthnSession(sessionID, sessionData)

	http.SetCookie(w, &http.Cookie{
		Name:     "webauthn_session",
		Value:    sessionID,
		Path:     "/api/webauthn/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(5 * time.Minute),
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(options)
}

func (a *API) WebAuthnLoginFinishHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("webauthn_session")
	if err != nil {
		http.Error(w, "Session cookie missing", http.StatusBadRequest)
		return
	}

	sessionData, ok := a.auth.GetWebAuthnSession(cookie.Value)
	if !ok {
		http.Error(w, "Session expired or not found", http.StatusBadRequest)
		return
	}
	defer a.auth.DeleteWebAuthnSession(cookie.Value)

	// Since we are using discoverable credentials, user ID is extracted from the credential itself.
	// We need to parse the response first to get the user handle
	parsedResponse, err := protocol.ParseCredentialRequestResponse(r)
	if err != nil {
		http.Error(w, "Failed to parse credential response", http.StatusBadRequest)
		return
	}
	
	// The user handle should be the userID
	userID := string(parsedResponse.Response.UserHandle)
	if userID == "" {
		http.Error(w, "No user handle found in credential", http.StatusBadRequest)
		return
	}

	loginResp, _, err := a.auth.FinishPasskeyLogin(userID, sessionData, r)
	if err != nil || !loginResp.Success {
		slog.Error("Failed to finish passkey login", "error", err)
		http.Error(w, loginResp.Message, http.StatusUnauthorized)
		return
	}

	// Set auth cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    loginResp.Token,
		Path:     "/",
		Expires:  time.Now().Add(time.Duration(loginResp.TokenExpiry) * time.Second),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(loginResp)
}

func (a *API) ListPasskeysHandler(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())
	
	passkeys, err := a.auth.ListPasskeys(userID)
	if err != nil {
		http.Error(w, "Failed to list passkeys", http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(passkeys)
}

func (a *API) DeletePasskeyHandler(w http.ResponseWriter, r *http.Request) {
	userID := UserIDFromContext(r.Context())
	
	// id is base64 encoded credential ID in the URL path.
	// But it's easier to pass it as a query param or in the URL path segment.
	// r.PathValue("id") requires Go 1.22
	idB64 := r.PathValue("id")
	if idB64 == "" {
		http.Error(w, "Missing passkey id", http.StatusBadRequest)
		return
	}
	
	credID, err := base64.RawURLEncoding.DecodeString(idB64)
	if err != nil {
		credID, err = base64.URLEncoding.DecodeString(idB64)
		if err != nil {
			http.Error(w, "Invalid passkey id format", http.StatusBadRequest)
			return
		}
	}
	
	err = a.auth.DeletePasskey(userID, credID)
	if err != nil {
		http.Error(w, "Failed to delete passkey", http.StatusInternalServerError)
		return
	}
	
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
