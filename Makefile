VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build test clean build-all

build:
	CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o bin/edge-node ./cmd/edge-node/

test:
	go test ./...

clean:
	rm -rf bin/

build-all:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o bin/edge-node-linux-amd64 ./cmd/edge-node/
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o bin/edge-node-linux-arm64 ./cmd/edge-node/
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o bin/edge-node-darwin-arm64 ./cmd/edge-node/
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -ldflags "-X main.version=$(VERSION)" -o bin/edge-node-windows-amd64.exe ./cmd/edge-node/
