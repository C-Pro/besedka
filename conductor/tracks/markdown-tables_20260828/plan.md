# Implementation Plan: Markdown Tables Support

## Phase 1: Backend Parser & Sanitizer (TDD)
- [x] Task: Write unit tests for Markdown tables in `internal/content/content_test.go`
    - [x] Add tests for basic tables, header rows, body rows
    - [x] Add tests for column alignments (left, center, right)
    - [x] Add tests for inline Markdown (links, bold, code) inside table cells
    - [x] Add tests for XSS and HTML injection attempts inside table cells
- [x] Task: Implement table support in `internal/content/content.go`
    - [x] Add `extension.Table` to `goldmark.New` parser
    - [x] Update `markdownPolicy` in `bluemonday` to allow `table`, `thead`, `tbody`, `tr`, `th`, `td` and alignment attributes (`align`, `style` if used)
- [x] Task: Conductor - User Manual Verification 'Phase 1: Backend Parser & Sanitizer' (Protocol in workflow.md)

## Phase 2: Frontend Styling & Layout
- [x] Task: Add CSS styling for markdown tables in `static/css/components.css`
    - [x] Style `table`, `thead`, `tbody`, `tr`, `th`, `td` for dark theme (`--border-subtle`, `--bg-panel`, `--bg-hover`, `--text-primary`)
    - [x] Add responsive horizontal scrolling (`overflow-x: auto`) for tables in `.message-content`
    - [x] Configure column width constraints (~20vw max-width target with word-wrapping)
    - [x] Ensure alignment styles (left, center, right) render as intended
- [x] Task: Verify mention decoration & link behavior inside table cells in `static/js/components/ChatWindow.js` and `mentions.js`
- [x] Task: Conductor - User Manual Verification 'Phase 2: Frontend Styling & Layout' (Protocol in workflow.md)

## Phase 3: Integration & End-to-End Verification
- [x] Task: Run full test suite and linters (`make check`)
- [x] Task: Manual browser verification of markdown tables rendered in Besedka web UI
- [x] Task: Conductor - User Manual Verification 'Phase 3: Integration & End-to-End Verification' (Protocol in workflow.md)
