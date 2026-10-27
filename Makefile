BINARY  := groot
PKG     := ./cmd/groot
VERSION ?= 0.6.0
LDFLAGS := -X main.version=$(VERSION)

.PHONY: all build test e2e lint vet fmt run clean cleanall release-check snapshot install-check demo

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

# install-check lints install.sh with shellcheck when it is installed. The
# installer's tests run with `make test`.
install-check:
	@if command -v shellcheck >/dev/null 2>&1; then shellcheck install.sh; else echo "shellcheck not installed: skipping install-check (brew install shellcheck)"; fi

# demo re-renders assets/demo.gif from assets/demo.tape with vhs (brew install vhs).
# It runs the built binary against a throwaway home so the recording never shows real data.
# The working directory must hold an Anthropic key in .env; no model call is made.
DEMO_HOME ?= $(HOME)/.cache/groot-demo
demo: build
	@command -v vhs >/dev/null 2>&1 || { echo "vhs is required: brew install vhs"; exit 1; }
	rm -rf "$(DEMO_HOME)" && mkdir -p "$(DEMO_HOME)"
	GROOT_DEMO_HOME="$(DEMO_HOME)" PATH="$(CURDIR)/bin:$$PATH" vhs assets/demo.tape
	rm -rf "$(DEMO_HOME)"
