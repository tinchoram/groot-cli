# Installing and running groot

Install groot, store its API key, check the setup, and run it.

## Install

On macOS or Linux, one command installs the latest release:

```sh
curl -fsSL https://raw.githubusercontent.com/tinchoram/groot-cli/main/install.sh | sh
groot auth login
```

The script downloads the archive for your platform, checks it against
`checksums.txt`, installs `groot` into `~/.local/bin`, and runs `groot doctor`.
Environment variables it reads:

- `GROOT_VERSION`: a release to install, with or without the `v` (default: latest).
- `GROOT_INSTALL_DIR`: where to put `groot` (default: `~/.local/bin`).
- `GITHUB_TOKEN` (or `GH_TOKEN`): optional. A GitHub token that raises the API
  rate limit; the script sends it with its requests when it is set.

If the script reports `release not found`, check the version and the releases page:
https://github.com/tinchoram/groot-cli/releases

### Manual install

Each release is on the GitHub releases page of `tinchoram/groot-cli`: one archive
per platform named `groot_<version>_<os>_<arch>.tar.gz` (`darwin` or `linux`,
`amd64` or `arm64`), and `checksums.txt` with the sha256 of each archive.

```sh
V=0.6.0; OS=darwin; ARCH=arm64        # your platform; V is the version without the v
gh release download "v$V" --repo tinchoram/groot-cli \
  --pattern "groot_${V}_${OS}_${ARCH}.tar.gz" --pattern checksums.txt
shasum -a 256 -c checksums.txt --ignore-missing   # must print OK
tar -xzf "groot_${V}_${OS}_${ARCH}.tar.gz" groot
mv groot ~/.local/bin/groot           # or /usr/local/bin/groot (needs sudo)
groot auth login
groot doctor
```

`groot upgrade` makes its requests without a token. If GitHub refuses one for its
rate limit, it retries once with `GITHUB_TOKEN` (or `GH_TOKEN`) when one is set,
so the token is optional and is never sent to GitHub unless a request is refused.

On Linux, groot's bash sandbox needs bubblewrap. `groot doctor` prints the install
command for your distribution when it is missing.

The packaging decisions, the planned install paths (install script, Homebrew
tap) and where every file lives are in [DISTRIBUTION.md](DISTRIBUTION.md).

## Setup

Requirements: Go 1.26 (the toolchain downloads automatically with
`GOTOOLCHAIN=auto`), and `kubectl` on `PATH` if you use the kubectl tool.

```sh
make build                 # writes bin/groot
bin/groot auth login       # prompts for the key with echo off, and stores it
bin/groot doctor           # checks the key, config files, sandbox and data
```

`groot auth login` stores the key in the macOS keychain (item `groot.anthropic`).
When the keychain is not available, and always on Linux, it writes
`~/.groot/credentials` with mode 0600. `--stdin` reads the key from the first line
of standard input: `echo "$KEY" | groot auth login --stdin`. On macOS the key is
passed to `security` as an argument, so it is briefly visible to `ps`.

groot looks for the key in this order and uses the first one it finds:

1. `ANTHROPIC_API_KEY` in the environment.
2. `ANTHROPIC_API_KEY` in a `.env` file in the working directory.
3. The macOS keychain.
4. `~/.groot/credentials`, a YAML line `anthropic_api_key: <key>`. A file that
   any other user can read is ignored, with a warning; fix it with `chmod 600`.

`groot auth status` prints the source and the last four characters of the key.
`groot auth logout` removes the stored key (keychain and credentials file). It never
touches the environment or `.env`.

`GROOT_HOME` moves the home directory (and so the credentials file and the global
config) as usual.

## Run

groot runs from any directory: it reads the global configuration in its home
(`~/.groot/groot.yaml`) and the project file of the working directory. See
[Configuration](CONFIGURATION.md).

```sh
bin/groot                              # reads ./groot.yaml when present
bin/groot --config ./prod.yaml --policy readonly
bin/groot --model claude-opus-5-5
bin/groot --version
bin/groot doctor                       # what groot found: key, config files, sandbox, kubectl, data
bin/groot upgrade --check              # is a newer release out? (no API key needed)
bin/groot upgrade                      # download it, verify the checksum, replace this binary
```

`groot upgrade` replaces the binary it runs from and says to restart groot. On
start, groot checks GitHub at most once a day, in the background, and shows a note
in the status line when a newer release exists. `updates.check: false` turns the
check off; `groot doctor` shows what the last check found, without a network call.

If no key is found, groot stops and names `groot auth login`.

Slash commands inside the UI: `/help`, `/model <name> [--agent]`, `/models`,
`/allow [clear]`, `/compact`, `/tokens`, `/clear`, `/exit`. Ctrl+C ends the session
the same way.

`/models` lists the models the Anthropic Models API offers, with their list price
per million tokens, and marks the current one. It gives up after 10 seconds.
`/model list` and `/model ls` do the same as `/models`.

`/model <id>` checks the id against that list, which is fetched once and kept for
10 minutes. An unknown id is refused with the three closest ids, and nothing
changes. `--force` sets the id without the check. `/model <id> --agent` also saves
the model into the active agent's file, after the same check. When the list cannot
be fetched, the id is accepted with a warning. Groot does the same with
`set_agent_model`. `groot.yaml` `provider.model` is checked at startup, and a
mismatch is a warning. Task routes warn about an unlisted model and still apply it.

If the provider answers that the current model does not exist, the conversation
switches back to the model before the last change, or to the one it started with,
prints `model <bad> not found · reverted to <prev>`, and retries the turn once.
