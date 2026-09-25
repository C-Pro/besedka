package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"besedka/internal/models"
)

func TestUpdateUserSettingsRejectsInvalidTheme(t *testing.T) {
	apiInstance := &API{}
	body := []byte(`{"notifications":{},"appearance":{"theme":"sepia"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/users/me/settings", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), userKey, models.User{ID: "user-1"}))
	recorder := httptest.NewRecorder()

	apiInstance.UpdateUserSettingsHandler(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
