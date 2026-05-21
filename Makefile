.PHONY: all check test lint lint-go lint-js test-go test-js e2e docker-build

all: check

check: lint test semgrep osv-scanner e2e

lint: lint-go lint-js

lint-go:
	docker run --rm \
		-v $(PWD):/app \
		-v $(shell go env GOCACHE):/root/.cache/go-build \
		-v $(shell go env GOMODCACHE):/go/pkg/mod \
		-v $(HOME)/.cache/golangci-lint:/root/.cache/golangci-lint \
		-w /app \
		-e GOFLAGS="-mod=vendor" \
		golangci/golangci-lint:v2.12.2 \
		golangci-lint run


lint-js:
	docker run --rm -w /app -v $(PWD)/static:/app/static:ro -v $(PWD)/biome.json:/app/biome.json:ro -v $(PWD)/.biomeignore:/app/.biomeignore:ro ghcr.io/biomejs/biome:2.3.13 lint static

test: test-go

test-go:
	go test -v -covermode=atomic -coverprofile=coverage.out -race ./...

e2e:
	go test -v -tags e2e ./e2e/...

semgrep:
	docker run --rm -v $(PWD):/src returntocorp/semgrep:1.106.0 semgrep scan --config=p/default

osv-scanner:
	docker run --rm -e GOTOOLCHAIN=auto -v $(PWD):/src -w /src ghcr.io/google/osv-scanner:latest -r .

docker-build:
	docker build -t ghcr.io/c-pro/besedka:latest .
