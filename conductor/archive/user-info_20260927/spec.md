# Specification - User Info (Time Zone & Preferred Language)

## 1. Overview
Besedka currently supports ephemeral user location updates over WebSocket when the "Share location" toggle is active. To enable localized experiences and context-aware bot interactions (e.g. scheduling, translation, regional greeting), clients and bots need a way to share the user's current time zone and preferred language.

This information is strictly ephemeral:
- Transmitted over WebSocket when the client has "Share location" enabled.
- Stored exclusively in server memory (`ws.Hub`) and never written to database storage (`bbolt`) or disk.
- Populated in the `GET /api/users` response for authorized callers (both human clients and bots).
- Cleared immediately when location sharing is toggled off or when the user disconnects / goes offline.

## 2. Functional Requirements

### 2.1 WebSocket Protocol Extension
- Add a dedicated client message type:
  `ClientMessageTypeUserInfo ClientMessageType = "userInfo"`
- Extend `models.ClientMessage` with:
  - `TimeZone string` (`json:"timeZone,omitempty"`)
  - `PreferredLanguage string` (`json:"preferredLanguage,omitempty"`)
  - `SharingEnabled *bool` (`json:"sharingEnabled,omitempty"`)
- Dispatching rules:
  - `userInfo` messages are routed globally in `ws.Connection` / `ws.Hub` without requiring a `chatId` (analogous to `location` messages).
  - When `SharingEnabled` is true (or non-nil true), the server associates the provided `timeZone` and `preferredLanguage` with the active session.
  - When `SharingEnabled` is false, the server clears the active in-memory user info for that session.
- Input validation:
  - Maximum length: 128 characters for `timeZone`, 64 characters for `preferredLanguage`.
  - Whitespace is trimmed, and control characters are stripped/rejected.
  - Plain canonical strings are stored (no HTML escaping in storage; clients render text securely).

### 2.2 In-Memory Storage & Multi-Session Lifecycle (`internal/ws/Hub`)
- Store user extra info contributions per connection in `Hub`:
  - Contribution map: `userExtraInfo map[string]map[chan models.ServerMessage]models.UserExtraInfo` protected by `h.mu`.
  - Each contribution records `{ TimeZone, PreferredLanguage, UpdatedAt }`.
- Multi-session semantics:
  - Multiple simultaneous connections (tabs/devices) for a single user update only their own connection entry.
  - Effective user info for a user is derived from their most recently updated active connection contribution.
  - When an individual connection sends `sharingEnabled: false` or disconnects (`leaveLocked`), only that connection's contribution is removed.
  - When the final connection for a user leaves (user goes offline), the user's entry is completely erased from memory.
  - Explicit administrative actions (`DisconnectUser`, `RemoveDeletedUser`) immediately purge all stored extra info for that user.
- Atomic Snapshot:
  - Provide an atomic snapshot method on `Hub` (e.g. `GetUsersStatusAndInfo()`) returning online status and effective user extra info in a single lock acquisition to avoid inconsistencies between online state and extra info.

### 2.3 API Response & Access Control (`GET /api/users`)
- Extend `models.User` with:
  - `TimeZone string` (`json:"timeZone,omitempty"`)
  - `PreferredLanguage string` (`json:"preferredLanguage,omitempty"`)
- Update `UsersHandler` (`internal/api/handlers.go`):
  - Enrich the user list with `timeZone` and `preferredLanguage` from the atomic Hub snapshot for online users with active contributions.
  - Offline users or users with sharing disabled omit `timeZone` and `preferredLanguage`.
  - Add `Cache-Control: private, no-store` header to `GET /api/users` response to prevent caching of dynamic privacy data.
- Access control:
  - Restrict `GET /api/users` to human users and bots (`api.RequireUserTypes(apiHandlers.UsersHandler, models.UserTypeHuman, models.UserTypeBot)`). Webhook accounts are forbidden from querying this endpoint.

### 2.4 Web Frontend Client (`static/js/state.js` & `components/InfoPanel.js`)
- Client state integration:
  - When `locationSharingEnabled` is true:
    - On initial WebSocket open (or reconnection), send a `userInfo` message with:
      - `timeZone`: resolved safely via `try/catch` from `Intl.DateTimeFormat().resolvedOptions().timeZone || ''`.
      - `preferredLanguage`: resolved from `navigator.language || (navigator.languages && navigator.languages[0]) || ''`.
  - In `toggleLocationSharing(enabled)`:
    - If enabled: call geolocation and send the `userInfo` message with `sharingEnabled: true`.
    - If disabled (or if geolocation permission is denied): send a `userInfo` message with `sharingEnabled: false`.
- Privacy scoping:
  - Clear or re-evaluate location sharing state on logout so session preferences do not bleed across user accounts on the same browser.

### 2.5 Bot Client Compatibility
- Bots connecting via WebSocket with a valid API token (`Authorization: Bearer <key>` or `token=bsk_...`) can send `userInfo` messages upon connect to declare their time zone and language.
- Bot clients calling `GET /api/users` receive the enriched user info of active users.

## 3. Non-Functional Requirements
- **Privacy & Ephemerality:** Time zone and language data are never written to the `bbolt` database or any persistent storage.
- **Zero Supply Chain Dependencies:** Implemented using standard Go standard library and native browser APIs (`Intl`, `navigator`).
- **Conformity:** Handlers conform to Go 1.22+ routing syntax and existing Besedka code style (minimal comments, clean self-documenting logic).

## 4. Acceptance Criteria
1. When a user connects with location sharing enabled, `GET /api/users` includes their `timeZone` and `preferredLanguage`.
2. When location sharing is toggled OFF (or geolocation denied), a `userInfo` message with `sharingEnabled: false` clears the data, and subsequent `GET /api/users` calls omit the fields.
3. When a user's final connection closes (user goes offline), their info is immediately purged from memory.
4. With multiple tabs open, closing one tab or toggling sharing does not corrupt the remaining tab's state.
5. Bot API-key callers receive enriched user info from `GET /api/users`; webhook tokens are denied access.
6. Database storage (`bbolt`) never contains `timeZone` or `preferredLanguage` data.
7. Automated tests cover protocol serialization, hub lifecycle, multi-session behavior, and API enrichment.
8. `make check` passes cleanly.

## 5. Out of Scope
- Persisting user timezone/language across server restarts.
- Auto-translating chat messages.
- Showing timezones directly in the main message timeline.
