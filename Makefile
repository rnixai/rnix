BINARY := crux
PKG := github.com/gonewx/crux

.PHONY: build install test lint vet clean all

build:
	go build -o $(BINARY) ./cmd/crux/

install:
	go install ./cmd/crux/

test:
	go test -race ./...

lint:
	golangci-lint run ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)

all: lint vet test build
