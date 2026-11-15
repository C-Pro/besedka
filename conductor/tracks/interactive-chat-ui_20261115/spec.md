# Specification - Interactive Chat UI Elements

## 1. Overview
Bots interacting in Besedka often need structured user input, such as approving or denying tool execution permissions or selecting from a list of options. This feature introduces lightweight interactive UI controls (`button`, `checkbox`, and `radio`) attached to chat messages, alongside a `ui_response` message type that records the user's choice, locks the original prompt across clients, and delivers the structured response and fallback text back to the bot.

## 2. Protocol & Data Architecture

### 2.1 Message Model Extensions (`internal/models/models.go`)
- Extend `MessageType`:
  ```go
  const (
      MessageTypeText       MessageType = "text"
      MessageTypeProgress   MessageType = "progress"
      MessageTypeUIResponse MessageType = "ui_response"
  )
  ```
- Define UI element types and styles:
  ```go
  type UIElementType string

  const (
      UIElementTypeButton   UIElementType = "button"
      UIElementTypeCheckbox UIElementType = "checkbox"
      UIElementTypeRadio    UIElementType = "radio"
  )

  type UIElementStyle string

  const (
      UIElementStyleDefault UIElementStyle = "default"
      UIElementStylePrimary UIElementStyle = "primary"
      UIElementStyleDanger  UIElementStyle = "danger"
  )

  type UIElement struct {
      ID          string         `json:"id"`
      Type        UIElementType  `json:"type"`
      Label       string         `json:"label"`
      Description string         `json:"description,omitempty"`
      Style       UIElementStyle `json:"style,omitempty"`
      Default     bool           `json:"default,omitempty"`
  }

  type UIData struct {
      Elements    []UIElement `json:"elements"`
      Answered    bool        `json:"answered,omitempty"`
      SelectedIDs []string    `json:"selectedIds,omitempty"`
      ResponderID string      `json:"responderId,omitempty"`
  }

  type UIResponseData struct {
      ParentSeq   int64    `json:"parentSeq"`
      SelectedIDs []string `json:"selectedIds"`
  }
  ```
- Wire formats:
  - `models.Message` & REST `POST /api/chats/{id}/messages`:
    - `UI *UIData `json:"ui,omitempty"``
    - `UIResponse *UIResponseData `json:"uiResponse,omitempty"``
  - `models.ClientMessage` (WebSocket `send`):
    - `MessageType MessageType `json:"messageType,omitempty"``
    - `UI *UIData `json:"ui,omitempty"``
    - `UIResponse *UIResponseData `json:"uiResponse,omitempty"``

### 2.2 Validation, Permissions & State Locking (`internal/ws/hub.go`, `internal/api/handlers.go`)
- **Sending Interactive UI (`ui != nil`)**:
  - Restricted to `UserTypeBot` on `text` messages. Non-bot users attempting to attach `ui` definitions are rejected (`403 Forbidden` via REST, dropped via WS).
  - Must contain 1 to 50 elements. Each element must have a unique non-empty `id`, a valid `type` (`button`, `checkbox`, `radio`), a non-empty `label`, and a valid `style` (empty/`default`, `primary`, `danger`). At most one `radio` element may have `default: true`.
  - Server initializes `Answered = false`, `SelectedIDs = nil`, `ResponderID = ""`.
- **Submitting a Response (`type = "ui_response"`)**:
  - Restricted to human users (`UserTypeHuman`) who are members of the chat.
  - Requires `uiResponse` with `parentSeq > 0` and at least one ID in `selectedIds` (or allows empty selection only when submitting unchecked checkboxes).
  - Server verifies that the parent message (`seq == parentSeq`) exists in the chat, has `ui != nil`, and has `ui.answered == false`. If already answered, the submission is rejected so the first response wins atomically.
  - Server validates that every ID in `selectedIds` exists in `parent.UI.Elements`, at most one `radio` ID is selected, and at most one `button` ID is selected.
  - **Fallback Text & Mention Routing**:
    - Client provides natural fallback text (e.g., `@bot Selected: Approve`). If `content` is empty, the server generates fallback text from the selected labels.
    - When the parent message author is a bot, the fallback text automatically includes `@<bot_username>` so standard mention extraction (`extractMentions`) delivers the `ui_response` to bots with `readMentions` enabled in Townhall.

### 2.3 Storage & Hydration (`internal/storage/`, `internal/chat/`)
- Extend `DBMessage` with `UI *DBUIData` and `UIResponse *DBUIResponseData` in `internal/storage/types.go`.
- When persisting a `ui_response` message in `bbolt.go` and adding to `chat.Chat` in `chat.go`, atomically update the parent message at `parentSeq` to set `UI.Answered = true`, `UI.SelectedIDs = selectedIds`, and `UI.ResponderID = userID`.
- Fetching history via REST (`GET /api/chats/{id}/messages`) or WebSocket (`join`/`fetch`) returns the parent message with its locked `answered` state and `selectedIds`, alongside the `ui_response` message in the timeline.

## 3. Client Frontend Architecture (`static/js/`)

### 3.1 Rendering Interactive Controls (`ChatWindow.js` & `components.css`)
- Messages keep standard Markdown-rendered `content` above an optional `.message-ui` container.
- Render elements in a clean vertical/button-row layout:
  - `checkbox` and `radio` elements render with label, optional secondary description, and initial checked state from `default`. Radio buttons within a message share a message-scoped group.
  - `button` elements render as action buttons styled with `.btn-default`, `.btn-primary`, or `.btn-danger` (with optional description).
  - If the message has `checkbox` or `radio` elements and **no** `button` elements, automatically append a primary "Submit" button.
- **Interaction Behavior**:
  - Clicking any `button` element immediately submits a `ui_response` containing all currently checked `checkbox`/`radio` IDs plus the clicked `button` ID.
  - Clicking the auto-generated "Submit" button submits all currently checked `checkbox`/`radio` IDs.
  - Fallback text is constructed as `@<bot_username> Selected: <comma-separated labels>` (or `Selected: None` if no checkboxes were selected).
- **Locked / Answered State**:
  - When `msg.ui.answered` is true (either on initial load or when a `ui_response` arrives via WebSocket), all inputs and buttons in `.message-ui` are disabled, selected elements are visually highlighted, and any auto-generated Submit button is hidden/disabled.
  - Bots viewing their own UI messages see controls in a read-only state.
- **`ui_response` Rendering**:
  - Rendered as a standard chat message line using its `content` fallback text.

## 4. Acceptance Criteria
1. Bots can send messages with markdown text and `ui` definitions (`button`, `checkbox`, `radio`).
2. Non-bot users attempting to send `ui` definitions are rejected with `403 Forbidden`.
3. Messages with checkboxes/radios and no buttons automatically render a Submit button; messages with explicit buttons submit both checked options and the clicked button immediately.
4. Submitting an interaction sends a `ui_response` message with `parentSeq`, `selectedIds`, and fallback text containing `@<bot_username>`.
5. Once answered, the original message updates in-place across all connected clients and persists its answered/disabled state and selected options across reloads.
6. Subsequent attempts to answer an already-answered message are rejected.
7. Bots receive `ui_response` messages via both WebSocket (`messages` event) and REST (`GET /api/chats/{id}/messages`), including in Townhall when the bot only has `readMentions`.
8. Full unit, integration, and E2E test coverage; `make check` passes cleanly.
