BINARY := bin/cuda-docs
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
BUILD_TAGS := netgo,osusergo

.PHONY: all build test vet fmt fmt-check clean install

all: fmt-check vet test build

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -tags "$(BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/cuda-docs

test:
	go test -race -coverprofile=coverage.out ./...

vet:
	go vet ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

fmt-check:
	@test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))" || \
		(echo "Go files are not formatted; run 'make fmt'" && exit 1)

install:
	CGO_ENABLED=0 go install -trimpath -tags "$(BUILD_TAGS)" -ldflags "$(LDFLAGS)" ./cmd/cuda-docs

clean:
	rm -rf bin dist coverage.out
