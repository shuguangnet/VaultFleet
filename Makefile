# Makefile
.PHONY: frontend-install frontend-build build-master build-agent build-all test docker-build docker-push clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
IMAGE ?= malabary/vaultfleet
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

frontend-install:
	cd web && npm install

frontend-build:
	cd web && npm run build

build-master: frontend-build
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/vaultfleet-master ./cmd/master

build-agent:
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/vaultfleet-agent ./cmd/agent

build-all: build-master build-agent

test:
	go test ./... -v -race -count=1

docker-build:
	docker build -t $(IMAGE):$(VERSION) -t $(IMAGE):latest -f build/Dockerfile .

docker-push:
	docker push $(IMAGE):$(VERSION)
	docker push $(IMAGE):latest

clean:
	rm -rf bin/
	go clean -cache -testcache
