# Makefile
.PHONY: build-master build-agent build-all test docker-build clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

build-master:
	CGO_ENABLED=1 go build $(LDFLAGS) -o bin/vaultfleet-master ./cmd/master

build-agent:
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/vaultfleet-agent ./cmd/agent

build-all: build-master build-agent

test:
	go test ./... -v -race -count=1

docker-build:
	docker build -t vaultfleet/master:$(VERSION) -t vaultfleet/master:latest -f build/Dockerfile .

clean:
	rm -rf bin/
	go clean -cache -testcache
