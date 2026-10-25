BINARY  := groot
PKG     := ./cmd/groot
VERSION ?= 0.5.4-dev
LDFLAGS := -X main.version=$(VERSION)

.PHONY: all build test e2e lint vet fmt run clean release-check snapshot

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

# Release tooling needs goreleaser (brew install goreleaser). The other targets do not.
# release-check validates .goreleaser.yaml and skips with a message when goreleaser is absent.
release-check:
	@if command -v goreleaser >/dev/null 2>&1; then goreleaser check; else echo "goreleaser not installed: skipping release-check (brew install goreleaser)"; fi

# snapshot builds every release archive locally into dist/, without publishing.
snapshot:
	@command -v goreleaser >/dev/null 2>&1 || { echo "goreleaser is required: brew install goreleaser"; exit 1; }
	goreleaser release --snapshot --clean --skip=publish
