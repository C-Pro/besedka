# Implementation Plan: Markdown Tables Support

## Phase 1: Backend Parser & Sanitizer (TDD)
- [ ] Task: Write unit tests for Markdown tables in `internal/content/content_test.go`
    - [ ] Add tests for basic tables, header rows, body rows
    - [ ] Add tests for column alignments (left, center, right)
    - [ ] Add tests for inline Markdown (links, bold, code) inside table cells
    - [ ] Add tests for XSS and HTML injection attempts inside table cells
- [ ] Task: Implement table support in `internal/content/content.go`
    - [ ] Add `extension.Table` to `goldmark.New` parser
    - [ ] Update `markdownPolicy` in `bluemonday` to allow `table`, `thead`, `tbody`, `tr`, `th`, `td` and alignment attributes (`align`, `style` if used)
- [ ] Task: Conductor - User Manual Verification 'Phase 1: Backend Parser & Sanitizer' (Protocol in workflow.md)

## Phase 2: Frontend Styling & Layout
- [ ] Task: Add CSS styling for markdown tables in `static/css/components.css`
    - [ ] Style `table`, `thead`, `tbody`, `tr`, `th`, `td` for dark theme (`--border-subtle`, `--bg-panel`, `--bg-hover`, `--text-primary`)
    - [ ] Add responsive horizontal scrolling (`overflow-x: auto`) for tables in `.message-content`
    - [ ] Configure column width constraints (~20vw max-width target with word-wrapping)
    - [ ] Ensure alignment styles (left, center, right) render as intended
- [ ] Task: Verify mention decoration & link behavior inside table cells in `static/js/components/ChatWindow.js` and `mentions.js`
- [ ] Task: Conductor - User Manual Verification 'Phase 2: Frontend Styling & Layout' (Protocol in workflow.md)

## Phase 3: Integration & End-to-End Verification
- [ ] Task: Run full test suite and linters (`make check`)
- [ ] Task: Manual browser verification of markdown tables rendered in Besedka web UI
- [ ] Task: Conductor - User Manual Verification 'Phase 3: Integration & End-to-End Verification' (Protocol in workflow.md)
