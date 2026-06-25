BINARY := rnix
PKG := github.com/rnixai/rnix
GITHUB_REPO := rnixai/rnix
GIT_VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo "0.1.0")
GIT_DIRTY := $(shell test -n "$$(git status --porcelain --ignore-submodules=dirty 2>/dev/null)" && echo "+dirty")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null)$(GIT_DIRTY)
BUILD_DATE := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -X main.ldVersion=$(GIT_VERSION) -X main.ldGitCommit=$(GIT_COMMIT) -X main.ldBuildDate=$(BUILD_DATE)

# A global proxy breaks direct `gh` calls; clear proxy vars for every gh invocation.
GH := NO_PROXY="*" HTTP_PROXY="" HTTPS_PROXY="" gh

.PHONY: build install test test-cover lint vet modernize modernize-check clean cache-clean all \
	changelog-check release-notes release publish release-watch help \
	gh-status gh-view gh-repo-edit gh-pr gh-pr-list gh-issue gh-issue-list gh-push
.DEFAULT_GOAL := help

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/rnix/

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/rnix/

test:
	go test -race ./...

# Mirrors CI's coverage stage (.github/workflows/test.yml). Coverage
# instrumentation changes runtime behavior, so some failures only surface
# under -cover — e.g. re-exec'd helper processes printing "GOCOVERDIR not set"
# into combined output. Run this before pushing to catch what `make test` can't.
test-cover:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	@echo "Coverage profile → coverage.out (view: go tool cover -html=coverage.out)"

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

# --- Release workflow ---

# Verify CHANGELOG.md has a section for VERSION (guards against publishing
# an undocumented release). Extracts the "## [x.y.z] - date" heading.
changelog-check:
	@test -n "$(VERSION)" || (echo "ERROR: VERSION is required. Usage: make changelog-check VERSION=0.10.1"; exit 1)
	@grep -qE '^## \[$(VERSION)\]' CHANGELOG.md || \
		(echo "ERROR: CHANGELOG.md has no '## [$(VERSION)]' section. Document the release first."; exit 1)
	@echo "==> CHANGELOG.md has a section for v$(VERSION)."

# Print the CHANGELOG body for VERSION (everything between this version's
# heading and the next "## " heading). Used as release notes.
release-notes:
	@test -n "$(VERSION)" || (echo "ERROR: VERSION is required. Usage: make release-notes VERSION=0.10.1"; exit 1)
	@awk '/^## \[$(VERSION)\]/{f=1;next} /^## /{f=0} f' CHANGELOG.md

release:
	@test -n "$(VERSION)" || (echo "ERROR: VERSION is required. Usage: make release VERSION=0.2.0"; exit 1)
	@echo "==> Validating version format..."
	@echo "$(VERSION)" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$' || (echo "ERROR: VERSION must be semver (e.g. 0.2.0)"; exit 1)
	@echo "==> Checking working tree is clean..."
	@test -z "$$(git status --porcelain --ignore-submodules=dirty)" || (echo "ERROR: working tree is not clean"; exit 1)
	$(MAKE) changelog-check VERSION=$(VERSION)
	@echo "==> Running tests..."
	$(MAKE) lint vet modernize-check test
	@echo "==> Creating tag v$(VERSION)..."
	git tag -a "v$(VERSION)" -m "Release v$(VERSION)"
	@echo "==> Building local binary (smoke test; GoReleaser builds the real artifacts)..."
	go build -ldflags "-X main.ldVersion=$(VERSION) -X main.ldGitCommit=$$(git rev-parse --short HEAD) -X main.ldBuildDate=$$(date -u '+%Y-%m-%dT%H:%M:%SZ')" -o $(BINARY) ./cmd/rnix/
	@echo ""
	@echo "Done! Release v$(VERSION) tagged and built locally."
	@echo "To publish (push tag → GoReleaser): make publish VERSION=$(VERSION)"

# Push the tag — this is the ONLY publish action. Pushing a `v*` tag triggers
# the Release workflow (.github/workflows/release.yml), which runs GoReleaser
# to build cross-platform archives + checksums and create the GitHub release.
# Do NOT create the release or upload assets here; GoReleaser owns that.
# Run after `make release VERSION=x.y.z`.
publish:
	@test -n "$(VERSION)" || (echo "ERROR: VERSION is required. Usage: make publish VERSION=0.2.0"; exit 1)
	@git rev-parse "v$(VERSION)" >/dev/null 2>&1 || (echo "ERROR: tag v$(VERSION) not found. Run 'make release VERSION=$(VERSION)' first."; exit 1)
	$(MAKE) changelog-check VERSION=$(VERSION)
	@echo "==> Pushing tag v$(VERSION) (triggers GoReleaser via GitHub Actions)..."
	git push origin "v$(VERSION)"
	@echo ""
	@echo "Done! Tag v$(VERSION) pushed."
	@echo "GoReleaser will build cross-platform archives and create the release."
	@echo "Watch progress: make release-watch"

# Watch the most recent Release workflow run (GoReleaser) to completion.
release-watch:
	$(GH) run watch $$($(GH) run list --workflow=release.yml --limit 1 --json databaseId --jq '.[0].databaseId')

# --- GitHub CLI (gh) ---
# All gh calls go through $(GH) to clear the global proxy that breaks gh.
gh-status: ## Check gh auth status
	$(GH) auth status

gh-view: ## Open repo in browser (rnixai/rnix)
	$(GH) repo view $(GITHUB_REPO) --web

gh-repo-edit: ## Update GitHub repo description, homepage, topics (sync repo view)
	$(GH) repo edit $(GITHUB_REPO) \
		--description "An operating system for AI agents — Unix philosophy: process, VFS, syscalls. Go 1.26." \
		--homepage "https://rnix.ai" \
		--add-topic "ai-agents" --add-topic "go" --add-topic "unix" \
		--add-topic "llm" --add-topic "mcp" --add-topic "orchestration" --add-topic "cli"

gh-pr: ## Create a PR (interactive)
	$(GH) pr create

gh-pr-list: ## List open PRs
	$(GH) pr list

gh-issue: ## Create an issue (interactive)
	$(GH) issue create

gh-issue-list: ## List open issues
	$(GH) issue list

gh-push: ## Push current branch and set upstream
	git push -u origin $$(git branch --show-current)

help: ## Show this help
	@printf "\033[1mUsage:\033[0m make [target]\n\n"
	@printf "\033[1mTargets:\033[0m\n"
	@printf "  \033[36m%-18s\033[0m %s\n" "build"           "Build binary → ./$(BINARY)"
	@printf "  \033[36m%-18s\033[0m %s\n" "install"         "Install binary to GOPATH/bin"
	@printf "  \033[36m%-18s\033[0m %s\n" "test"            "Run all tests with race detection"
	@printf "  \033[36m%-18s\033[0m %s\n" "test-cover"      "Run tests with coverage (mirrors CI)"
	@printf "  \033[36m%-18s\033[0m %s\n" "lint"            "Run golangci-lint"
	@printf "  \033[36m%-18s\033[0m %s\n" "vet"             "Run go vet"
	@printf "  \033[36m%-18s\033[0m %s\n" "modernize"       "Apply go fix modernizations"
	@printf "  \033[36m%-18s\033[0m %s\n" "modernize-check" "Check for pending modernizations"
	@printf "  \033[36m%-18s\033[0m %s\n" "all"             "lint + vet + modernize-check + test + build"
	@printf "  \033[36m%-18s\033[0m %s\n" "clean"           "Remove build artifacts"
	@printf "  \033[36m%-18s\033[0m %s\n" "cache-clean"     "Clean lint and Go caches"
	@printf "\n  \033[1mRelease (VERSION=x.y.z):\033[0m\n"
	@printf "  \033[36m%-18s\033[0m %s\n" "changelog-check" "Verify CHANGELOG.md has a section for VERSION"
	@printf "  \033[36m%-18s\033[0m %s\n" "release-notes"   "Print the CHANGELOG body for VERSION"
	@printf "  \033[36m%-18s\033[0m %s\n" "release"         "Validate + test + tag + build a release"
	@printf "  \033[36m%-18s\033[0m %s\n" "publish"         "Push tag → GoReleaser builds & publishes (after release)"
	@printf "  \033[36m%-18s\033[0m %s\n" "release-watch"   "Watch the GoReleaser run to completion"
	@printf "\n  \033[1mGitHub (gh):\033[0m\n"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-status"       "Check gh auth status"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-view"         "Open rnixai/rnix in browser"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-repo-edit"    "Sync repo description, homepage, topics"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-pr"           "Create a PR (interactive)"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-pr-list"      "List open PRs"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-issue"       "Create an issue (interactive)"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-issue-list"   "List open issues"
	@printf "  \033[36m%-18s\033[0m %s\n" "gh-push"         "Push current branch and set upstream"
	@printf "  \033[36m%-18s\033[0m %s\n" "help"            "Show this help"
