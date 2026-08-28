# Product Guide: Self-Improving General Use AI Agent (Phase 1: Request/Response Bot)

## Vision & Overview
The goal of this project is to build an autonomous, continuous-learning general-use AI agent integrated with the Besedka self-hosted chat system. The long-term architecture spans 13 phases ranging from hybrid context retrieval, tool execution, and Docker sandboxes to offline PESO LoRA training, automated validation, and curiosity-driven dreaming.

Phase 1 establishes the bedrock: a simple, robust request/response agent daemon written in Go. The agent connects to Besedka, listens for direct mentions or messages, queries Gemini via an OpenAI-compatible REST API, and responds in chat.

## Initial Concept
Self-improving general use AI agent for Besedka chat. Phase 1 delivers a request/response bot responding to direct mentions via Google Gemini API (OpenAI compatible endpoint), fully tested locally and integrated into a CI/CD pipeline mirroring Besedka's deployment strategy.

## Key Features & Capabilities (Phase 1)
- **Besedka Ingress Gateway (Go):** Listens to Besedka API / WebSocket events for incoming chat messages.
- **Configurable Mention Filter:** Detects direct mentions based on a configurable handle (e.g., `@bot`, defaulting via `BOT_HANDLE` env var) in Townhall and direct messages.
- **OpenAI-Compatible LLM Client (Go):** Routes structured requests to Google Gemini OpenAI-compatible REST endpoint (`https://generativelanguage.googleapis.com/v1beta/openai/`) using `gemini-3.7-flash` by default (configurable via `GEMINI_MODEL`, `GEMINI_API_KEY`, and `BASE_URL`).
- **Besedka Egress Response:** Posts generated text back to the originating Besedka chat via REST/WebSocket API.
- **Local-First Testing & Harness:** Complete local test setup allowing manual browser testing against a local Besedka instance before any cloud deployment.

## Deployment & CI/CD Strategy
- **Co-located Spot VM Deployment:** Bot containers/processes deploy on the **same GCP Spot VMs** as the Besedka instances (test bot co-located with test Besedka, prod bot co-located with prod Besedka).
- **Main Branch Deployment (Test):** Pushes/merges to `main` trigger automated linting, security scans, unit tests, and build/deploy to the test Spot VM.
- **SemVer Tag Deployment (Prod):** Tagging a main commit with `v*` triggers release build, container registry push, and rolling update deployment to the production Spot VM.
- **Automated Quality Checks:** Pipeline includes `golangci-lint`, OSV Scanner, Semgrep, unit/integration tests, and local integration verification.

## Architectural Principles
- **Pragmatic Code Structure:** Keep code simple and direct. Do NOT create premature abstractions or interfaces. Introduce interfaces only when their design is proven or when strictly required for clean unit testing.
- **Robust Integration Testing:** Emphasize thorough local integration tests and end-to-end testing alongside unit tests.
- **Modular Go Base:** Organize cleanly into package boundaries (`internal/ingress`, `internal/llm`, `internal/config`) without unnecessary interface indirections.
- **Zero Third-Party Vendor Lock:** Self-hosted Go binary/container running directly alongside Besedka.
