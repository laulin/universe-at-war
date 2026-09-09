.PHONY: build release art test test-race vet lint audit bench check

GO ?= go
VERSION ?= dev
LDFLAGS = -s -w -X main.version=$(VERSION)

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/universe-at-war ./cmd/universe-at-war

# release builds the whole matrix, one self-contained binary per target, with
# no CGO so nothing outside the file is ever needed.
release:
	@set -e; for target in linux/amd64 linux/arm64 windows/amd64 darwin/amd64 darwin/arm64; do \
		os=$${target%%/*}; arch=$${target##*/}; \
		suffix=""; if [ "$$os" = "windows" ]; then suffix=".exe"; fi; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" \
			-o bin/universe-at-war-$(VERSION)-$$os-$$arch$$suffix ./cmd/universe-at-war; \
	done

# art regenerates the embedded illustrations from the masters in images/. It
# is a developer target: the derivatives are committed, so nothing that builds
# or runs the game needs an image toolchain.
art:
	./scripts/build-art.sh

# The race detector instruments a SQLite transpiled to Go, which costs the suite
# roughly two orders of magnitude. It stays well inside this budget, but not
# inside the ten minutes go test allows by default.
test:
	$(GO) test -timeout 30m ./...

test-race:
	$(GO) test -race -timeout 30m ./...

vet:
	$(GO) vet ./...

lint:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@2026.2.1 ./...

audit:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

bench:
	$(GO) test -bench=. -benchtime=20x -run=^$$ ./internal/domain/... ./tests/

check: test vet
	@test -z "$$(gofmt -l .)"
