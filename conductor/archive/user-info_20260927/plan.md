# Implementation Plan - User Info (Time Zone & Preferred Language)

## Overview
Implement ephemeral user info (`timeZone` and `preferredLanguage`) support across data models, WebSocket client messaging, in-memory hub storage, `GET /api/users` enrichment, and the web frontend client.

## User Tasks & Milestones

- [x] Task 1: Protocol & Model Extensions
  - [x] Add `ClientMessageTypeUserInfo` constant and fields (`timeZone`, `preferredLanguage`, `sharingEnabled`) to `models.ClientMessage`
  - [x] Add `TimeZone` and `PreferredLanguage` fields to `models.User` with `omitempty`
  - [x] Write unit tests in `internal/models/models_test.go` verifying JSON serialization, deserialization, and omission of empty values

- [x] Task 2: Hub In-Memory Storage & Multi-Session Lifecycle
  - [x] Write unit tests in `internal/ws/hub_test.go` covering `userInfo` message dispatch, multi-session contributions, explicit clear (`sharingEnabled: false`), and connection disconnect cleanup
  - [x] Implement `UserExtraInfo` struct and per-connection tracking in `ws.Hub` protected by `h.mu`
  - [x] Implement `handleUserInfo` in `ws.Hub` with string length validation and sanitization
  - [x] Route `ClientMessageTypeUserInfo` in `ws.Connection.processClientMessage` and `ws.Hub.Dispatch`
  - [x] Update `ws.Hub.leaveLocked`, `DisconnectUser`, and `RemoveDeletedUser` to clean up connection contributions and purge user state when offline
  - [x] Implement atomic snapshot method (`GetUsersStatusAndInfo`) on `ws.Hub`

- [x] Task 3: API Integration & Access Control
  - [x] Write tests in `internal/api/handlers_test.go` (or `internal/api/api_apikey_test.go` / `main_test.go`) for `GET /api/users` verifying user info enrichment, webhook rejection, and cache control headers
  - [x] Update `UsersHandler` in `internal/api/handlers.go` to use atomic snapshot and populate `TimeZone` / `PreferredLanguage`
  - [x] Restrict `GET /api/users` route in `internal/http/apiServer.go` to human and bot user types
  - [x] Add `Cache-Control: private, no-store` header to `GET /api/users` response
  - [x] Write regression test confirming `timeZone` and `preferredLanguage` are never written to `storage.BboltStorage`

- [x] Task 4: Web Frontend Client Updates
  - [x] In `static/js/state.js`, add helper to safely extract IANA timezone and preferred language
  - [x] In `static/js/state.js`, send `userInfo` message on WebSocket open and reconnect when location sharing is enabled
  - [x] In `static/js/state.js`, update `toggleLocationSharing` to send `userInfo` with `sharingEnabled: true` when enabled and `sharingEnabled: false` when disabled or on geolocation error
  - [x] In `static/js/state.js`, clean up location sharing consent on logout to prevent cross-account leakage

- [x] Task 5: End-to-End Verification & Quality Review
  - [x] Run full test suite with race detector (`go test -race ./...`)
  - [x] Run `make check` to ensure all linters, formatting, and tests pass cleanly
  - [x] Manually verify behavior in browser at `http://localhost:8080` (connect, toggle share location, inspect `GET /api/users`)
  - [x] Review implementation against GEMINI.md rules and project styleguides
