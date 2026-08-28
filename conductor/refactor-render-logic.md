# Refactor Render Logic for Besedka

## Objective
Eliminate the aggressive `innerHTML` rendering approach in the frontend UI to prevent massive re-renders, input disruptions, and image flickering. Shift to a fine-grained, vanilla JS DOM manipulation strategy (surgical DOM updates), ensuring components only react to relevant state changes while adhering strictly to the project's zero-build-step philosophy.

## Scope & Impact
*   **Target:** Frontend rendering logic, primarily `ChatWindow.js` and `ChatList.js`.
*   **Impact:** Significantly improved UI performance, elimination of typing disruptions (cursor jumping, focus loss), and smooth image loading without spinner flickering. E2E tests must remain stable.

## Implementation Steps

### Phase 1: State Change Isolation
Currently, `store.subscribe` triggers full re-renders on *any* state change. We will implement strict "diffing" within the component render functions.
*   Track previous state values (e.g., `prevChatId`, `prevMessagesLength`) within component closures.
*   Only execute DOM updates if the specific data slice relevant to the component has changed.

### Phase 2: Refactor `ChatWindow.js` (Core Surgical Updates)
This is the most critical component causing the flickering.
*   **Static Shell Initialization:** Build the structural HTML (`.chat-header`, `.messages-container`, `.input-area`) exactly **once** upon component creation.
*   **Chat Switching:** When `state.activeChatId` changes, update the header data, clear `.messages-container`, reset `lastRenderedSeq`, and render the new chat's message history.
*   **Append-Only Message Rendering:**
    *   Track the highest message `seq` rendered (`lastRenderedSeq`).
    *   When new messages arrive for the active chat, identify only the messages where `seq > lastRenderedSeq`.
    *   Use `document.createElement` (or a `template` tag) to build individual message nodes.
    *   Append new nodes directly to `.messages-container` via `appendChild`.
*   **Input Preservation:** The `#message-input` textarea must remain untouched by incoming messages or background state updates, ensuring zero disruption to typing or IME composition.
*   **Attachment Handling:** Update the attachment button icon (spinner vs. paperclip) and attachment count without recreating the entire `.input-area`.

### Phase 3: Refactor `ChatList.js`
*   Build the list of chats initially.
*   On subsequent state updates:
    *   Iterate through existing `.chat-item` nodes.
    *   Toggle the `.active` class based on `state.activeChatId`.
    *   Update presence indicators (`.chat-preview`) and unread counts (`.unread-badge`) directly using DOM properties (`textContent`, `classList`).
    *   If a new chat is added (e.g., a new DM), append the new item without rebuilding the whole list.

### Phase 4: Refactor `InfoPanel.js` (Optional but recommended)
*   Ensure the location toggle and map updates don't cause full re-renders of the panel.

## Verification & Testing
1.  **Manual Verification:**
    *   **Image Re-render Check:** Explicitly test that sending a new message does *not* cause existing images in the chat log to re-render. Existing image elements must remain untouched in the DOM, and their `onload` events must not re-fire.
    *   **Typing Check:** Type a long message while another user sends messages. Verify no cursor jumping or input loss.
    *   Verify unread counts and presence indicators update without redrawing the UI.
2.  **E2E Tests:**
    *   The structural DOM hierarchy (`.chat-header h3`, `#message-input`, `.messages-container`, `.chat-item`) must be preserved to ensure compatibility with existing E2E tests.
    *   Run `e2e/typing_test.go` to explicitly validate the typing disruption fix.
    *   Run the complete E2E suite (`go test -v ./e2e/...`) and address any selector or timing issues caused by the shift to asynchronous DOM rendering.
