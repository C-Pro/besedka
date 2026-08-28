# Development & Deployment Workflow

## 1. General Principles
- **Local-First Development:** Every feature must be built, tested, and verified locally before merging or deploying to remote environments.
- **No Premature Abstraction:** Write straightforward, pragmatic Go code. Use interfaces only when proven necessary or required for clean testing.
- **Continuous Quality Enforcement:** Maintain test coverage and pass all linter and security checks before creating pull requests or merging.

## 2. Development Workflow (Local Iteration)
1. **Track / Feature Implementation:**
   - Develop features in focused iterations.
   - Write corresponding unit tests covering success and failure paths.
2. **Local Verification:**
   - Run `go test -v -covermode=atomic -race ./...` to verify unit tests.
   - Run `make check` (or equivalent lint and test suite) to ensure code compliance.
   - Run local integration tests against a locally running Besedka instance (`http://localhost:8080`).
3. **Manual Browser Verification:**
   - Test bot mention handling and response generation manually in the browser UI against the local Besedka instance.

## 3. CI/CD & Deployment Strategy (Parity with Besedka)
1. **Main Branch Merge (Test Environment Deployment):**
   - Merging or pushing code to the `main` branch triggers the GitHub Actions CI/CD pipeline.
   - Pipeline steps: Semgrep scan, OSV dependency scanner, `golangci-lint`, unit tests, and integration tests.
   - Upon successful quality checks, the pipeline builds the Docker container and triggers a rolling deployment to the **Test GCP Spot VM** (co-located with test Besedka).
2. **SemVer Release Tagging (Production Deployment):**
   - Tagging a commit on `main` with a semantic versioning tag (`v*`, e.g., `v1.0.0`) triggers the Production Deployment workflow.
   - Upon successful build and test execution, the release Docker image (`v1.0.0` and `latest`) is pushed to GCP Artifact Registry, and a rolling update deploys the image to the **Production GCP Spot VM** (co-located with production Besedka).

## 4. Quality & Self-Review Protocol
- Run linter and tests after every non-trivial code modification.
- Review changes to ensure no sensitive secrets or credentials are leaked in code, logs, or error responses.
- Ensure all new HTTP handlers use Go 1.22+ routing syntax (`mux.HandleFunc("METHOD /path", handler)`).
