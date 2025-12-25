.PHONY: all check test lint lint-go lint-js test-go test-js

all: check

check: lint test

lint: lint-go lint-js

lint-go:
	golangci-lint run

lint-js:
	npm run lint

test: test-go test-js

test-go:
	go test -v -covermode=atomic -coverprofile=coverage.out -race ./...

test-js:
	npm test
