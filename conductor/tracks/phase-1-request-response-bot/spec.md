# Track Spec: Phase 1 - Simple Request/Response Agent & Besedka Gateway

## Context & Purpose
Phase 1 implements a lightweight, self-contained Go daemon that acts as an AI chat agent for Besedka. The agent connects to Besedka via WebSocket / HTTP REST API, monitors incoming chat messages for direct mentions (e.g., `@bot` in Townhall or direct messages), generates responses via Google Gemini's OpenAI-compatible REST API (`https://generativelanguage.googleapis.com/v1beta/openai/`), and posts answers back into the chat.

## Requirements & Scope
- **Besedka Gateway (Go):** Connect to Besedka API (`/api/chat` WebSocket and REST endpoints) using `fasthttp/websocket`. Authenticate bot requests cleanly.
- **Mention Detection & Ingress Filtering:** Monitor messages in Townhall and DMs. Respond only when directly mentioned (`@bot` or configured `BOT_HANDLE`) or in DM channels.
- **LLM Provider Integration (Gemini OpenAI Endpoint):** Build an HTTP REST client targeting Gemini's OpenAI-compatible completions endpoint (`/v1/chat/completions`) using model `gemini-3.7-flash` (configurable via `GEMINI_MODEL`, `GEMINI_API_KEY`, `BASE_URL`).
- **Response Egress & Limits:** Post responses back to Besedka chat. Enforce response length limits (max 2 paragraphs / 8 sentences for Townhall; up to 10 paragraphs for DMs).
- **Transient Error Retry:** Retry failed Gemini API calls up to 3 times with exponential backoff before sending a friendly fallback error response.
- **Local Testing Setup:** Local test scripts/harness to run and test the bot against local Besedka instance (`http://localhost:8080`).
- **CI/CD & Co-located Deployment:**
  - Build containerized bot image via Docker multi-stage build.
  - GitHub Actions pipeline (`pipeline.yml`): Push to `main` deploys to GCP Test Spot VM (co-located with test Besedka instance); tagging `v*` on `main` deploys to GCP Production Spot VM (co-located with prod Besedka instance).
  - Code quality checks: `golangci-lint`, OSV scanner, Semgrep, unit tests, integration tests.

## Out of Scope (Phase 1)
- Long conversation history ring buffer / vector storage (Phase 2).
- Tool execution / code sandboxes (Phases 3-4).
- Dynamic LoRA fine-tuning / local weights (Phases 7-10).
