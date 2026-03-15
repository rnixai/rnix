BINARY := rnix
PKG := github.com/rnixai/rnix
GITHUB_REPO := rnixai/rnix
GIT_VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "0.1.0")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null)
BUILD_DATE := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -X main.version=$(GIT_VERSION) -X main.gitCommit=$(GIT_COMMIT) -X main.buildDate=$(BUILD_DATE)

.PHONY: build install test lint vet modernize modernize-check clean cache-clean all release help \
	gh-status gh-view gh-repo-edit gh-pr gh-pr-list gh-issue gh-issue-list gh-release-publish gh-push
.DEFAULT_GOAL := help

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

# --- GitHub CLI (gh) ---
gh-status: ## Check gh auth status
	gh auth status

gh-view: ## Open repo in browser (rnixai/rnix)
	gh repo view $(GITHUB_REPO) --web

gh-repo-edit: ## Update GitHub repo description, homepage, topics (sync repo view)
	gh repo edit $(GITHUB_REPO) \
		--description "An operating system for AI agents — Unix philosophy: process, VFS, syscalls. Go 1.26." \
		--homepage "https://rnix.ai" \
		--add-topic "ai-agents" --add-topic "go" --add-topic "unix" \
		--add-topic "llm" --add-topic "mcp" --add-topic "orchestration" --add-topic "cli"

gh-pr: ## Create a PR (interactive)
	gh pr create

gh-pr-list: ## List open PRs
	gh pr list

gh-issue: ## Create an issue (interactive)
	gh issue create

gh-issue-list: ## List open issues
	gh issue list

gh-release-publish: ## Create GitHub release (after make release VERSION=x.y.z). Usage: make gh-release-publish VERSION=0.2.0
	@test -n "$(VERSION)" || (echo "ERROR: VERSION is required. Usage: make gh-release-publish VERSION=0.2.0"; exit 1)
	gh release create "v$(VERSION)" $(BINARY) --title "v$(VERSION)" --notes "Release v$(VERSION)"

gh-push: ## Push current branch and set upstream
	git push -u origin $$(git branch --show-current)

help: ## Show this help
	@printf "\033[1mUsage:\033[0m make [target]\n\n"
	@printf "\033[1mTargets:\033[0m\n"
	@printf "  \033[36m%-18s\033[0m %s\n" "build"           "Build binary → ./$(BINARY)"
	@printf "  \033[36m%-18s\033[0m %s\n" "install"         "Install binary to GOPATH/bin"
	@printf "  \033[36m%-18s\033[0m %s\n" "test"            "Run all tests with race detection"
	@printf "  \033[36m%-18s\033[0m %s\n" "lint"            "Run golangci-lint"
	@printf "  \033[36m%-18s\033[0m %s\n" "vet"             "Run go vet"
	@printf "  \033[36m%-18s\033[0m %s\n" "modernize"       "Apply go fix modernizations"
	@printf "  \033[36m%-18s\033[0m %s\n" "modernize-check" "Check for pending modernizations"
	@printf "  \033[36m%-18s\033[0m %s\n" "all"             "lint + vet + modernize-check + test + build"
	@printf "  \033[36m%-18s\033[0m %s\n" "clean"           "Remove build artifacts"
	@printf "  \033[36m%-18s\033[0m %s\n" "cache-clean"     "Clean lint and Go caches"
	@printf "  \033[36m%-18s\033[0m %s\n" "release"         "Tag and build a release (VERSION=x.y.z)"
	@printf "\n  \033[1mGitHub (gh):\033[0m\n"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-status"       "Check gh auth status"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-view"         "Open rnixai/rnix in browser"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-repo-edit"    "Sync repo description, homepage, topics"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-pr"           "Create a PR (interactive)"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-pr-list"      "List open PRs"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-issue"       "Create an issue (interactive)"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-issue-list"   "List open issues"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-release-publish" "Create GitHub release (VERSION=x.y.z)"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-push"         "Push current branch and set upstream"
	@printf "  \033[36m%-18s\033[0m %s\n" "help"            "Show this help"
