.PHONY: build test test-race vet check

GO ?= go

build:
	$(GO) build -trimpath -o bin/universe-at-war ./cmd/universe-at-war

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

check: test vet
	@test -z "$$(gofmt -l .)"
