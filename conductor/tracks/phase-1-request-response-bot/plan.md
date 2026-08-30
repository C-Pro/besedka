# Implementation Plan: Phase 1 - Simple Request/Response Agent & Besedka Gateway

## Overview
Build and deploy a Go-based request/response bot for Besedka, integrating Gemini API via OpenAI-compatible endpoints, with local-first testing and a CI/CD pipeline matching Besedka's deployment model.

## User Tasks & Milestones

- [x] Task 1: Project Scaffolding & Configuration Module
  - [x] Implement configuration module (`internal/config`) loading `BOT_HANDLE`, `BESEDKA_URL`, `BESEDKA_API_KEY`, `GEMINI_API_KEY`, `GEMINI_MODEL`, `BASE_URL`.
  - [x] Write unit tests for configuration loading and validation.

- [x] Task 2: OpenAI-Compatible LLM Client (`internal/llm`)
  - [x] Implement REST client for `/v1/chat/completions` targeting Gemini OpenAI endpoint (`https://generativelanguage.googleapis.com/v1beta/openai/`).
  - [x] Implement 3x exponential backoff retry mechanism for transient HTTP errors.
  - [x] Write unit tests with HTTP mock server testing completions and retry logic.

- [x] Task 3: Besedka Ingress & Egress Gateway (`internal/gateway`)
  - [x] Implement WebSocket connection to Besedka `/api/chat` using `fasthttp/websocket`.
  - [x] Implement mention parser (`@bot` / `BOT_HANDLE` detection in Townhall and DMs).
  - [x] Implement response length formatting rules (2 paragraphs max for Townhall, 10 paragraphs max for DMs).
  - [x] Implement message posting egress back to Besedka API.
  - [x] Write unit tests for mention extraction, message formatting, and gateway logic.

- [x] Task 4: Bot Agent Main Entry Point & Local Harness
  - [x] Implement `cmd/agent/main.go` bringing together config, gateway, and LLM client.
  - [x] Create local test script / compose setup to run agent against local Besedka instance (`http://localhost:8080`).
  - [x] Write local integration test verifying full mention -> Gemini call -> Besedka reply cycle.

- [x] Task 5: Dockerization & CI/CD Deployment Pipeline
  - [x] Create multi-stage Dockerfile for agent binary.
  - [x] Update GitHub Actions workflow (`.github/workflows/pipeline.yml`) to run tests/linting and build/deploy agent container co-located on GCP Spot VMs (test on `main` push, prod on `v*` tag).
  - [x] Verify `make check` passes cleanly.
