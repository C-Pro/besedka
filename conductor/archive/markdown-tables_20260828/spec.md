# Specification - Markdown Tables Support

## 1. Overview
Besedka's message formatting pipeline uses `goldmark` and `bluemonday` to render Markdown into secure HTML. With LLM bot integration, chat responses frequently contain GitHub Flavored Markdown (GFM) tables. Currently, tables are not enabled in goldmark and table HTML elements are stripped by bluemonday. This track enables GFM tables in the backend markdown parser, updates HTML sanitization policies to permit table elements/attributes, adds custom CSS styling for tables matching Besedka's dark theme, and ensures responsive rendering in the chat UI.

## 2. Functional Requirements
- **Parser Extension:** Enable `extension.Table` in the backend `goldmark` parser (`internal/content/content.go`).
- **HTML Sanitization:** Configure `bluemonday.Policy` (`markdownPolicy`) to permit table HTML elements (`table`, `thead`, `tbody`, `tr`, `th`, `td`) and alignment attributes (e.g. `align`, `style` or text alignment as produced by goldmark).
- **Inline Elements in Cells:** Retain support for existing inline elements inside table cells (`strong`, `em`, `code`, `a`, mentions via `decorateMentions`).
- **UI & CSS Styling:**
  - Provide clean, dark-mode table styling in `static/css/components.css` adhering to Besedka's color tokens (`--border-subtle`, `--bg-panel`, `--bg-hover`, `--text-primary`, `--text-secondary`).
  - Distinct styling for header cells (`th`) vs body cells (`td`), borders, padding, and subtle alternating row backgrounds or border dividers.
  - Column width constraints: Target column width limits (e.g. `max-width: 20vw` or min/max column constraints with word wrapping `overflow-wrap: break-word` / `word-break: break-word`).
  - Table container / wrapper or table `overflow-x: auto` ensuring wide tables horizontally scroll cleanly within the message bubble without breaking the chat layout.
- **Frontend Mentions & Links:** Ensure mention decoration (`decorateMentions`) and external link handling function properly within table cells.

## 3. Non-Functional Requirements
- **Security:** Maintain strict sanitization — no raw/unsafe HTML or script injection via table elements or malformed markdown.
- **Performance:** Negligible overhead for messages without tables; efficient rendering for messages with multiple or large tables.
- **Zero New Dependencies:** Goldmark's built-in `extension.Table` and `bluemonday` are already vendored/in `go.mod`.

## 4. Acceptance Criteria
1. Markdown table syntax (e.g., `| Header 1 | Header 2 |\n|---|---|\n| Cell 1 | Cell 2 |`) renders as an HTML `<table>` element with matching rows and headers.
2. Cell alignments (left, center, right) in markdown table dividers (`:---`, `:---:`, `---:`) are preserved and rendered correctly.
3. Inline markdown elements (code blocks, bold, links, mentions) render properly inside table cells.
4. Tables fit the dark theme aesthetically and scroll horizontally if they exceed available viewport width, while wrapping words appropriately.
5. Unit tests in `internal/content/content_test.go` cover various table formats, alignments, inline formatting, and sanitization edge cases.
6. Local manual browser verification confirms proper rendering and responsive behavior in the UI.
7. `make check` passes cleanly.

## 5. Out of Scope
- Interactive table sorting or editing in the web client.
- Exporting tables to CSV/Excel from UI.
