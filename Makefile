BINARY := rnix
PKG := github.com/rnixai/rnix

.PHONY: build install test lint vet modernize modernize-check clean cache-clean all

build:
	go build -o $(BINARY) ./cmd/rnix/

install:
	go install ./cmd/rnix/

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
