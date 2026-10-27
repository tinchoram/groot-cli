<h1 align="center">Groot</h1>

<p align="center"><em>A terminal agent for developers and platform engineers. Kubernetes, logs, files and shell, behind a permission gate.</em></p>

<p align="center">
  <a href="https://github.com/tinchoram/groot-cli/actions/workflows/ci.yml"><img src="https://github.com/tinchoram/groot-cli/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/tinchoram/groot-cli/releases"><img src="https://img.shields.io/github/v/release/tinchoram/groot-cli" alt="Latest release"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/tinchoram/groot-cli" alt="Go version">
  <img src="https://img.shields.io/github/license/tinchoram/groot-cli" alt="License: MIT">
  <a href="https://pkg.go.dev/github.com/tinchoram/groot-cli"><img src="https://pkg.go.dev/badge/github.com/tinchoram/groot-cli.svg" alt="Go Reference"></a>
</p>

<p align="center"><img src="assets/demo.gif" alt="groot demo" width="800"></p>

## What it does

- **Safety**: every tool call the agent makes passes a permission gate, and bash runs in an OS sandbox. The terminal column is your own shell and is neither gated nor audited. See [Safety](docs/SAFETY.md).
- **Spaces and agents**: work in a directory with its own agents, tools and audit log. See [Spaces and agents](docs/SPACES-AND-AGENTS.md).
- **Memory**: agents keep scoped long-term memory in SQLite. See [Memory](docs/MEMORY.md).
- **ODD flow**: each task is sized, then moves through implement, verify and deliver. See [ODD flow](docs/FLOW.md).
- **Integrations**: fetch web pages and call MCP servers behind an allowlist. See [Integrations](docs/INTEGRATIONS.md).
- **Sessions**: history per agent, background conversations and messages between agents. See [Sessions](docs/SESSIONS.md).

## Install

On macOS or Linux, one command installs the latest release into `~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/tinchoram/groot-cli/main/install.sh | sh
```

Set `GROOT_INSTALL_DIR` to install somewhere else. Or, with Go installed:

```sh
go install github.com/tinchoram/groot-cli/cmd/groot@latest
```

Manual install, upgrades and API key setup are in [docs/INSTALL.md](docs/INSTALL.md).

## Quickstart

`groot` is the administrator agent: it creates spaces and agents and explains how groot works.

Try it:

1. Ask groot to create a space: "create a space for ~/src/payments". groot
   confirms the name, path and purpose, then creates it after your approval.
2. The space appears in the SPACES section of the sidebar. Press `tab` to focus
   the sidebar, `s` to jump to SPACES, pick it with `↑`/`↓`, and press `enter`.
   The space switches and its first agent is selected.
3. Add an agent: ask groot for one, or run `/agent new reviewer "reviews diffs"`,
   edit the file it prints, and assign it with `/space assign reviewer`.
   Press `a` in the sidebar to see the agents, select one, and talk to it.

More in [docs/GETTING-STARTED.md](docs/GETTING-STARTED.md).

## Documentation

| Topic | File |
| --- | --- |
| Install script, manual install, API key setup, running groot | [`docs/INSTALL.md`](docs/INSTALL.md) |
| First run, screen layout, keys, mouse and work mode | [`docs/GETTING-STARTED.md`](docs/GETTING-STARTED.md) |
| groot.yaml fields, context window and spend budgets | [`docs/CONFIGURATION.md`](docs/CONFIGURATION.md) |
| Permission gate, bash sandbox, audit log and untrusted content | [`docs/SAFETY.md`](docs/SAFETY.md) |
| Spaces, agents, agent files, tools, identity and the library | [`docs/SPACES-AND-AGENTS.md`](docs/SPACES-AND-AGENTS.md) |
| ODD flow: task sizes, phases, evidence and delegation | [`docs/FLOW.md`](docs/FLOW.md) |
| Session history, background conversations and agent messaging | [`docs/SESSIONS.md`](docs/SESSIONS.md) |
| Long-term memory scopes, tools, preamble and /memory | [`docs/MEMORY.md`](docs/MEMORY.md) |
| Reading the web, MCP servers, and bug and feature reports | [`docs/INTEGRATIONS.md`](docs/INTEGRATIONS.md) |
| Development commands, end-to-end tests, and extension guides | [`docs/CONTRIBUTING.md`](docs/CONTRIBUTING.md) |
| Package layout, per-turn wiring, gate chain and storage | [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) |
| Packaging decisions, install paths and data locations | [`docs/DISTRIBUTION.md`](docs/DISTRIBUTION.md) |
| Shipped releases and planned work | [`docs/ROADMAP.md`](docs/ROADMAP.md) |

## Development

```sh
make build
make test
```

Contributing and architecture: [docs/CONTRIBUTING.md](docs/CONTRIBUTING.md) and [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## License

groot is MIT licensed, see [LICENSE](LICENSE). Release binaries bundle third-party
code under the licenses listed in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
