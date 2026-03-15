BINARY := rnix
PKG := github.com/rnixai/rnix
GIT_VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "0.1.0")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null)
BUILD_DATE := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -X main.version=$(GIT_VERSION) -X main.gitCommit=$(GIT_COMMIT) -X main.buildDate=$(BUILD_DATE)

.PHONY: build install test lint vet modernize modernize-check clean cache-clean all release

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/rnix/

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/rnix/

test:
	go test -race ./...

lint:
	golangci-lint run --allow-parallel-runners ./...

vet:
	go vet ./...

modernize:
	go fix ./...

modernize-check:
	@diff=$$(go fix -diff ./... 2>&1); \
	if [ -n "$$diff" ]; then \
		echo "$$diff"; \
		echo "Run 'make modernize' to apply fixes"; \
		exit 1; \
	fi

clean:
	rm -f $(BINARY)

cache-clean:
	golangci-lint cache clean
	go clean -cache -testcache

all: lint vet modernize-check test build

release:
	@test -n "$(VERSION)" || (echo "ERROR: VERSION is required. Usage: make release VERSION=0.2.0"; exit 1)
	@echo "==> Validating version format..."
	@echo "$(VERSION)" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$' || (echo "ERROR: VERSION must be semver (e.g. 0.2.0)"; exit 1)
	@echo "==> Checking working tree is clean..."
	@test -z "$$(git status --porcelain)" || (echo "ERROR: working tree is not clean"; exit 1)
	@echo "==> Running tests..."
	$(MAKE) lint vet modernize-check test
	@echo "==> Creating tag v$(VERSION)..."
	git tag -a "v$(VERSION)" -m "Release v$(VERSION)"
	@echo "==> Building release binary..."
	go build -ldflags "-X main.version=$(VERSION) -X main.gitCommit=$$(git rev-parse --short HEAD) -X main.buildDate=$$(date -u '+%Y-%m-%dT%H:%M:%SZ')" -o $(BINARY) ./cmd/rnix/
	@echo ""
	@echo "Done! Release v$(VERSION) tagged and built."
	@echo "To publish: git push origin v$(VERSION)"
