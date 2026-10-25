BINARY  := groot
PKG     := ./cmd/groot
VERSION ?= 0.5.3
LDFLAGS := -X main.version=$(VERSION)

.PHONY: all build test e2e lint vet fmt run clean

all: build test

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

test:
	go test ./... -count=1

# End-to-end tests against the real API: needs ANTHROPIC_API_KEY, costs a few cents.
e2e:
	go test -tags e2e ./e2e/... -count=1 -v

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
