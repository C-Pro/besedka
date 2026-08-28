# Technology Stack: Self-Improving General Use AI Agent

## 1. Programming Language & Core Runtime
- **Language:** Go (1.26+)
- **Concurrency & I/O:** Standard library (`net/http`, `context`, `sync`, `encoding/json`, `time`)
- **Coding Standards:** Go 1.22+ routing style (`mux.HandleFunc("POST /...", ...)`), idiomatic Go formatting (`gofmt`, `go vet`)

## 2. Ingress & Networking (Besedka Integration)
- **Protocol:** HTTP REST & JSON over WebSocket
- **WebSocket Library:** `github.com/fasthttp/websocket` for high-performance WebSocket client connection and event streaming
- **API Authentication:** HTTP Authorization Bearer token header (`Authorization: Bearer <API_KEY>`) / session tokens for user/bot actions

## 3. LLM Provider & Inference API
- **Endpoint Protocol:** OpenAI-compatible REST API (`/v1/chat/completions`)
- **Primary Provider Target (Phase 1):** Google Gemini OpenAI-compatible REST API (`https://generativelanguage.googleapis.com/v1beta/openai/`)
- **Default Model:** `gemini-3.7-flash` (configurable via `GEMINI_MODEL`)
- **Configurability:** Environment variables for `GEMINI_API_KEY`, `GEMINI_MODEL`, `BASE_URL`
- **Future Readiness:** Compatible with local open-weights inference servers (vLLM / SGLang) running local models in future phases.

## 4. Configuration & State Management
- **Configuration:** Environment variables (`env` / `os.Getenv`)
- **State Storage (Phase 1):** In-memory state & message queues (preparing for SQLite / BoltDB in Phase 2)

## 5. Testing & Code Quality
- **Testing Framework:** Native Go `testing` package, `go test -race -cover`
- **Integration & E2E Testing:** Go integration tests running against local Besedka instance (`http://localhost:8080`)
- **Static Analysis & Security:** `golangci-lint`, `osv-scanner`, `semgrep`

## 6. Containerization & Deployment Infrastructure
- **Containerization:** Multi-stage Dockerfile based on `golang:1.26-alpine` build step and minimal runtime image (`alpine:latest` / `distroless`)
- **Deployment Platform:** GCP Spot VM (co-located on the same Spot VM instances as Besedka)
- **Container Registry:** GCP Artifact Registry (`asia-southeast2-docker.pkg.dev`)
- **CI/CD Orchestration:** GitHub Actions (`pipeline.yml` with `workflow_dispatch`, `push` to `main` for test VM, and `v*` tag triggers for prod VM)
