package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"besedka/internal/models"
)

func decodeResp(t *testing.T, rec *httptest.ResponseRecorder) models.APIResponse {
	t.Helper()
	var resp models.APIResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

func TestHandleBackupDisabled(t *testing.T) {
	s := &AdminServer{} // onBackup nil => S3 disabled
	rec := httptest.NewRecorder()
	s.handleBackup(rec, httptest.NewRequest(http.MethodPost, "/api/backup", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if resp := decodeResp(t, rec); resp.Success || resp.Message != "S3 backup not enabled" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHandleBackupSuccess(t *testing.T) {
	called := false
	s := &AdminServer{onBackup: func(context.Context) error { called = true; return nil }}
	rec := httptest.NewRecorder()
	s.handleBackup(rec, httptest.NewRequest(http.MethodPost, "/api/backup", nil))

	if rec.Code != http.StatusOK || !called {
		t.Fatalf("status = %d called = %v, want 200/true", rec.Code, called)
	}
	if resp := decodeResp(t, rec); !resp.Success {
		t.Fatalf("expected success, got %+v", resp)
	}
}

func TestHandleBackupError(t *testing.T) {
	s := &AdminServer{onBackup: func(context.Context) error { return errors.New("upload boom") }}
	rec := httptest.NewRecorder()
	s.handleBackup(rec, httptest.NewRequest(http.MethodPost, "/api/backup", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if resp := decodeResp(t, rec); resp.Success {
		t.Fatalf("expected failure, got %+v", resp)
	}
}

func TestHandleShutdownSuccess(t *testing.T) {
	var exitErr error
	exitCalled := false
	s := &AdminServer{
		onShutdown:  func(context.Context) (bool, error) { return true, nil },
		triggerExit: func(err error) { exitCalled = true; exitErr = err },
	}
	rec := httptest.NewRecorder()
	s.handleShutdown(rec, httptest.NewRequest(http.MethodPost, "/api/shutdown", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !exitCalled || exitErr != nil {
		t.Fatalf("triggerExit called=%v err=%v, want true/nil", exitCalled, exitErr)
	}
	if resp := decodeResp(t, rec); !resp.Success {
		t.Fatalf("expected success, got %+v", resp)
	}
}

func TestHandleShutdownBackupFailureExitsNonZero(t *testing.T) {
	boom := errors.New("backup failed after 3 attempts")
	var exitErr error
	exitCalled := false
	s := &AdminServer{
		onShutdown:  func(context.Context) (bool, error) { return false, boom },
		triggerExit: func(err error) { exitCalled = true; exitErr = err },
	}
	rec := httptest.NewRecorder()
	s.handleShutdown(rec, httptest.NewRequest(http.MethodPost, "/api/shutdown", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	// The process must still be told to exit, and with a non-nil error so it
	// exits non-zero.
	if !exitCalled || exitErr == nil {
		t.Fatalf("triggerExit called=%v err=%v, want true/non-nil", exitCalled, exitErr)
	}
	if resp := decodeResp(t, rec); resp.Success {
		t.Fatalf("expected failure response, got %+v", resp)
	}
}

func TestHandleShutdownUnavailable(t *testing.T) {
	s := &AdminServer{} // ops not wired
	rec := httptest.NewRecorder()
	s.handleShutdown(rec, httptest.NewRequest(http.MethodPost, "/api/shutdown", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}
