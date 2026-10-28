# Project context

groot is a terminal agent for developers and platform engineers, written in Go.
It works inside spaces (directories) that have their own agents, tools, audit log
and memory, behind a permission gate.

Groot loads this file from the working directory into the system prompt. Keep it
short and factual. Do not put secrets here.

## Build and test

- `make build` writes the dev binary to `bin/groot` (untracked).
- `GROOT_REQUIRE_SANDBOX=1 go test -race ./... -count=1` runs the tests. The variable
  turns a missing sandbox into a failure instead of a skip, as CI does.
- `make lint` runs `go vet` and a gofmt check. `make e2e` calls the real API and needs
  `ANTHROPIC_API_KEY`: do not run it by default.

## Architecture

- Hexagonal layout. The core packages (`agent`, `api`, `gate`, `compact`, `tool`)
  depend on ports and the standard library only: no vendor SDK and no terminal UI.
- `internal/arch/arch_test.go` enforces these import boundaries. A violation fails
  `go test`.

## Docs

- `docs/*.md` is the reference. Start with `docs/ARCHITECTURE.md` and
  `docs/CONFIGURATION.md`.
- `docs/tasks/` holds feature documents, one per feature.
- `docs/ROADMAP.md` is the open backlog.
- User-visible changes go under `## Unreleased` in `CHANGELOG.md`.

## Commits

- Conventional commits: `feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`.
- No AI attribution in commit messages or pull request descriptions.
