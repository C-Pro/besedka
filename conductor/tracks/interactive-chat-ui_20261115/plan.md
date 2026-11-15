# Implementation Plan: Interactive Chat UI Elements

## Phase 1: Data Models, Storage & Backend Validation
- [x] Task: Extend protocol models and BoltDB storage (`internal/models/`, `internal/storage/`, `internal/chat/`)
    - [x] Define `MessageTypeUIResponse`, `UIElementType` (`button`, `checkbox`, `radio`), `UIElementStyle` (`default`, `primary`, `danger`), `UIElement`, `UIData`, and `UIResponseData` in `internal/models/models.go`
    - [x] Add `UI` and `UIResponse` fields to `models.Message` and `models.ClientMessage`
    - [x] Add `DBUIElement`, `DBUIData`, and `DBUIResponseData` to `internal/storage/types.go` and update `DBMessage` msgpack serialization
    - [x] Update `UpsertMessage` and `ListMessages` in `internal/storage/bbolt.go` to persist `UI` and `UIResponse` fields and atomically update the parent message's `UI.Answered`, `UI.SelectedIDs`, and `UI.ResponderID` when a `ui_response` is saved
    - [x] Update `ChatRecord`, `AddRecord`, and `GetRecords` in `internal/chat/chat.go` to carry `UI` and `UIResponse` and update the in-memory parent record when a `ui_response` is added
    - [x] Write unit tests in `internal/models/models_test.go`, `internal/storage/bbolt_test.go`, and `internal/chat/chat_test.go`
- [x] Task: Implement API & WebSocket validation, permissions, and bot delivery (`internal/ws/hub.go`, `internal/api/handlers.go`, `API.md`)
    - [x] Validate `UI` definitions in `Hub.SendMessage`: restrict sending `ui` to `UserTypeBot`, validate element counts, unique IDs, types, labels, styles, and single radio default
    - [x] Validate `ui_response` messages in `Hub.SendMessage`: restrict to `UserTypeHuman`, verify parent message exists, has `ui != nil`, is not yet answered, and validate `selectedIds` against parent elements
    - [x] Ensure fallback `content` is populated (auto-generating from selected labels and prepending `@<bot_username>` when replying to a bot) so standard mention routing delivers `ui_response` to bots with `readMentions` in Townhall
    - [x] Update `SendMessageHandler` in `internal/api/handlers.go` to accept `ui` and `uiResponse` in REST requests and return appropriate status codes (`403 Forbidden`, `400 Bad Request`, `409 Conflict` when already answered)
    - [x] Document the new `ui` and `ui_response` message formats in `API.md`
    - [x] Write unit and integration tests in `internal/ws/hub_test.go` and `internal/http/bot_webhook_integration_test.go`
- [x] Task: Conductor - User Manual Verification 'Phase 1: Data Models, Storage & Backend Validation' (Protocol in workflow.md)

## Phase 2: Frontend Rendering, State Locking & End-to-End Verification
- [x] Task: Update frontend state management and submission (`static/js/state.js`)
    - [x] Update `handleNewMessages` in `static/js/state.js` so arriving `ui_response` messages lock the corresponding parent message in-place (`ui.answered = true`, `ui.selectedIds`, `ui.responderId`) and increment `messageRevision` while appending the `ui_response` message to the chat stream
    - [x] Add `sendUIResponse(chatId, parentSeq, selectedIds, fallbackContent)` action in `static/js/state.js` to send `ui_response` over WebSocket
- [x] Task: Render interactive UI controls and answered states (`static/js/components/ChatWindow.js`, `static/css/components.css`)
    - [x] Render `.message-ui` below formatted markdown `.message-content` for messages with `msg.ui`
    - [x] Support `checkbox`, `radio`, and `button` elements with descriptions, styles (`default`, `primary`, `danger`), and default states
    - [x] Auto-render a "Submit" button when `checkbox` or `radio` elements are present without any explicit `button` elements
    - [x] Handle immediate submission on button click (including any checked checkbox/radio IDs) and auto-Submit button click, generating `@<bot_username> Selected: ...` fallback text
    - [x] Render disabled/locked state with highlighted selected items when `msg.ui.answered` is true (and update in-place when `messageRevision` changes)
    - [x] Add dark and light theme styles for `.message-ui` controls in `static/css/components.css`
- [x] Task: End-to-end & quality verification
    - [x] Add integration/E2E tests verifying bot UI rendering, button/checkbox/radio interactions, state locking across clients/reloads, and bot receipt via WebSocket and REST
    - [x] Run `make check` and manually verify in the browser at `http://localhost:8080`
- [x] Task: Conductor - User Manual Verification 'Phase 2: Frontend Rendering, State Locking & End-to-End Verification' (Protocol in workflow.md)
