BINARY  := groot
PKG     := ./cmd/groot
VERSION ?= 0.6.0
LDFLAGS := -X main.version=$(VERSION)

.PHONY: all build test e2e lint vet fmt run clean cleanall notices release-check snapshot install-check demo

all: build test

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)

test:
	go test -race ./... -count=1

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

# cleanall deletes ~/.groot: the global config, every space's database and audit
# log, and the agents. It asks for confirmation (y or yes); FORCE=1 skips the
# question for scripts: make cleanall FORCE=1
cleanall:
	@if [ "$(FORCE)" = "1" ]; then \
		rm -rf ~/.groot && echo "removed ~/.groot"; \
	else \
		printf 'Delete ~/.groot (config, spaces, agents, databases, audit logs)? [y/N] '; \
		read answer; \
		case "$$answer" in \
			y|yes) rm -rf ~/.groot && echo "removed ~/.groot" ;; \
			*) echo "aborted: ~/.groot kept"; exit 1 ;; \
		esac; \
	fi

# notices checks that THIRD_PARTY_NOTICES.md names every module of the groot binary.
# It does not rewrite the file: the notices were built by hand (see its Method section),
# and no tool reproduces them on the current Go toolchain. Add a row and an appendix
# text for each module it reports.
notices:
	@command -v go >/dev/null 2>&1 || { echo "go not installed: skipping notices"; exit 0; }
	@missing=0; \
	for m in $$(go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}' $(PKG) | sort -u); do \
		grep -qF "| $$m |" THIRD_PARTY_NOTICES.md || { echo "missing from THIRD_PARTY_NOTICES.md: $$m"; missing=1; }; \
	done; \
	if [ "$$missing" = 1 ]; then exit 1; fi; \
	echo "THIRD_PARTY_NOTICES.md names every module of the binary"

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
