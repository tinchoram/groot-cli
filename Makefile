BINARY  := groot
PKG     := ./cmd/groot
VERSION ?= 0.3.0-dev
LDFLAGS := -X main.version=$(VERSION)

.PHONY: all build test lint vet fmt run clean

all: build test

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

test:
	go test ./... -count=1

vet:
	go vet ./...

lint: vet
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed for:"; gofmt -l .; exit 1)

fmt:
	gofmt -w .

run:
	go run $(PKG) $(ARGS)

clean:
	rm -rf bin

cleanall:
	rm -rf ~/.groot
