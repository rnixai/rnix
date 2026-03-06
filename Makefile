BINARY := rnix
PKG := github.com/rnixai/rnix

.PHONY: build install test lint vet clean all

build:
	go build -o $(BINARY) ./cmd/rnix/

install:
	go install ./cmd/rnix/

test:
	go test -race ./...

lint:
	golangci-lint run ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)

all: lint vet test build
