# Distribution

Decisions taken on 2026-10-25 about how groot is packaged, installed and what a
fresh installation contains. What is already built is marked (done); the rest
is the plan for the first public release.

## Decisions

1. **The repository is public from 0.6.0.** Downloads and `groot upgrade` need
   no token. `GITHUB_TOKEN` is optional: it only raises the GitHub API rate limit,
   and `groot upgrade` sends it only when GitHub refuses a request. A 404 for a
   release means the version does not exist; the message points to the releases
   page.
2. **Platforms: macOS and Linux**, `amd64` and `arm64`. Windows is deferred: the
   binary compiles there, but the bash sandbox and the keychain do not exist,
   so it needs a design of its own, not just a build target.
3. **The first public release is a polished one.** 0.5.4 was tagged by mistake
   and the tag was removed before anything was published. 0.5.5 was never
   published either: 0.6.0 is the first public release, cut from main on
   2026-10-25. No GitHub release exists yet.
4. **A fresh installation starts with the `root` space and the `groot` agent
   only.** `root` points at the home directory; `groot` is the administrator that
   creates spaces and agents and explains the keys. Nothing else is configured.
5. **The `debug` agent is not installed by default.** It is the agent that keeps
   the groot backlog (`~/.groot/feedback`), useful to us while we build groot.
   It ships in the built-in library, so groot installs it like any other agent
   when asked ("install debug"), or the user runs `/agent install debug`. Bootstrap
   neither creates nor assigns it. The user puts it in the space they choose, and
   the feedback flow (`/bug`, `/feat`, `send_message` to `debug`) follows it there;
   when debug is assigned to no space, those commands fail with a message that says
   how to install it. An installed debug is kept current like groot, cannot be
   renamed, and can be deleted (its backlog files stay). Existing homes keep their
   debug file and assignments untouched.
6. **The API key is never stored in plain configuration.** `groot auth login`
   keeps it in the macOS keychain, or in `~/.groot/credentials` with mode 0600
   on Linux. The environment and a `.env` in the working directory still win,
   for CI and development (done).
7. **Configuration is layered**: defaults, then `~/.groot/groot.yaml`, then the
   project's `groot.yaml` (done).
8. **Releases are built by goreleaser from a tag** (`v*`): one archive per
   platform plus `checksums.txt`, release notes taken from the embedded
   changelog by `cmd/relnotes` (done; exercised locally with a snapshot, and the
   GitHub release workflow published v0.6.0 on 2026-10-26).
9. **`groot upgrade` replaces the binary in place** after verifying the sha256;
   a daily cached check shows a short notice when a newer version exists (done).
10. **No Gatekeeper signing for now.** Binaries installed through the install
    script or brew are not quarantined; a browser download would be. Signing and
    notarization need an Apple developer account and are postponed.

## Install paths

| Path | Platform | Needs the repo public | Status |
|---|---|---|---|
| `curl -fsSL https://raw.githubusercontent.com/tinchoram/groot-cli/main/install.sh \| sh` | macOS, Linux | no | done, `install.sh` at the repo root and in each release |
| `brew tap tinchoram/groot && brew install groot` | macOS, Linux | yes | planned; goreleaser `brews:` pushes the formula to `tinchoram/homebrew-groot` |
| Manual: download the archive, check the sum, move the binary | macOS, Linux | no | done, see README "Install" |
| `.deb` / `.rpm` through goreleaser `nfpms:` | Linux | yes | later |
| `go install github.com/tinchoram/groot-cli/cmd/groot@latest` | any with Go | yes | works; no sandbox hint, so not the recommended path |

System dependencies: on Linux, bubblewrap for the bash sandbox (`groot doctor`
prints the distro's install command); `kubectl` only if the kubectl tool is
used. macOS needs nothing: `sandbox-exec` ships with the system.

## What a fresh install contains

On the first run groot creates:

- `~/.groot/spaces.yaml` with the `root` space at the home directory and `groot`
  assigned to it.
- `~/.groot/agents/groot.md`, the managed administrator agent. Its prompt may be
  edited; the file is never deleted.
- The welcome screen with the keys. groot is selected and ready.

It asks for nothing except the API key, which is missing until
`groot auth login` runs (or `ANTHROPIC_API_KEY` is exported).

## Where the data lives

| What | Where |
|---|---|
| The binary | wherever it is installed (`~/.local/bin/groot`, brew's prefix) |
| Agent definitions | `~/.groot/agents/<name>.md` (YAML front matter: model, tools, description; body: the prompt) |
| Spaces and assignments | `~/.groot/spaces.yaml` |
| Messages between agents | `~/.groot/inbox.db` |
| Backlog (when debug is installed) | `~/.groot/feedback/` |
| Prompt history, layout, update cache | `~/.groot/history`, `layout.yaml`, `update-check.json` |
| Global configuration | `~/.groot/groot.yaml` (optional) |
| Your name (`/me`, first-start prompt) | `~/.groot/user.yaml`, mode 0600 |
| API key | macOS keychain, or `~/.groot/credentials` (0600) on Linux |
| Memory, sessions, tasks and audit of a space | `<space dir>/.groot/groot.db` and `audit.jsonl`, inside the project |
| The same for `root` | `~/.groot/groot.db`, `~/.groot/audit.jsonl` |

Consequences:

- A project's memory travels with the project directory. Whether `.groot/` is
  committed or ignored is a per-project choice; groot does not touch
  `.gitignore`.
- Everything global moves by copying `~/.groot`; the key does not, run
  `groot auth login` again on the new machine. `GROOT_HOME` points all of it
  elsewhere, which is how the tests and a trial installation stay isolated.
- Uninstalling is removing the binary and, if wanted, `~/.groot` and the
  `.groot/` directories of the projects. Nothing else is written.

### What moves and what stays

- `delete_space` keeps a space's `.groot` data. With `purge_data` and `confirm`
  it deletes `<space dir>/.groot` (the memory database and the audit log) and
  nothing else in the project.
- `move_agent` copies the agent's memory, sessions and tasks to the target
  space's database only when `with_memory` is set. It never moves them: the
  source keeps its copies.
- `edit_space` never moves data. A new path leaves the `.groot` data under the
  old path, and the space reads its data from the new path from then on.

## Order of the remaining work

1. `install.sh` for macOS and Linux (done: detects OS and arch, downloads, verifies
   the checksum, installs to `~/.local/bin`, runs `groot doctor`).
2. First public release, 0.6.0: the repository is public, `v0.6.0` is tagged and
   the release workflow published it on 2026-10-26. Still open: check
   `groot upgrade --check` against that release.
3. Homebrew tap through goreleaser.
4. Later: `.deb`/`.rpm`, signing and notarization, Windows.
