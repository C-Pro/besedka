.PHONY: all check test lint lint-go lint-js test-go test-js

all: check

check: lint test

lint: lint-go lint-js

lint-go:
	golangci-lint run


lint-js:
	docker run --rm -w /app -v $(PWD)/static:/app/static:ro -v $(PWD)/biome.json:/app/biome.json:ro -v $(PWD)/.biomeignore:/app/.biomeignore:ro ghcr.io/biomejs/biome:2.3.13 lint static

test: test-go

test-go:
	go test -v -covermode=atomic -coverprofile=coverage.out -race ./...
