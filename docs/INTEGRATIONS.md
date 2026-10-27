# Integrations

Reporting bugs and feature requests, reading the web, and MCP servers.

## Reporting bugs and features

Groot keeps its own backlog in `<GROOT_HOME>/feedback/`. `BACKLOG.md` is an index
table, regenerated after every change. Each item is one file under `items/`, named
`BUG-001-<slug>.md` or `FEAT-001-<slug>.md`, with frontmatter (id, type, status,
area, reported_at, space, agent, version) and its description.

- `/bug <text>` and `/feat <text>` send a message to the debug agent, in whatever
  space it is assigned to, from the current space and agent. When debug is not
  installed they fail with a message that says how to install it. No model call and no permission gate. The body holds your text,
  the last ten lines you typed in this conversation as context, and the version.
  Earlier reports are left out. The confirmation gives the message id.
- Only the `debug` agent writes the backlog. A fresh home does not create it: install
  it from the library (`install_agent debug` or `/agent install debug`) into the
  space you choose. Bootstrap never moves or assigns it, and an existing home keeps
  its debug file and assignments. Once installed it is kept current like groot and
  cannot be renamed. Deleting it keeps the backlog files. For each report it
  checks `feedback_list` for duplicates and writes the item with `feedback_report`.
  It records silently: it replies to the reporter only when the report asks for a
  confirmation, or when `feedback.confirm: true` is set in `groot.yaml`. Then it
  replies with `<ID> recorded` or `duplicate of <ID>`. The reply rule is added to
  the debug agent's system prompt at each start, so a stored prompt from an older
  version follows it too. It marks items done or
  wontfix with `feedback_update` when you ask. With work mode on for debug, the
  report starts a turn by itself.
- Every agent can read the backlog with `feedback_list`. Other agents do not have
  `feedback_report` or `feedback_update`; they report by messaging debug.
- `/backlog [open|done|all]` prints the index (open items by default).
- Release sweep: when you ask debug to "sweep the backlog" or "close the release",
  it calls `read_changelog`, which returns the `CHANGELOG.md` embedded in the groot
  binary, so the sweep works from any space. It marks each open item that a shipped
  entry covers as done, with the note `shipped in <version>: <line>`. Entries under
  a heading marked `(unreleased)` or with a `-dev` version are not shipped and close
  nothing. Test probes become wontfix. Items it cannot match stay open and are
  listed in its answer.
- Statuses are open, in_progress, done and wontfix.

## Reading the web

`fetch_url` reads a page or API response over http or https. It is a read-only
GET with a 15 second timeout, at most 5 redirects, a 1 MB body cap and the
User-Agent `groot-cli/<version>`. No cookies and no credentials are sent.

- HTML becomes readable text: headings as `#`, paragraphs, list items as `-`,
  links as `text (url)` and table rows with cells joined by ` | `. Scripts,
  styles, navigation and footers are dropped. Other types must be `text/*` or
  `application/json`; anything else returns `unsupported content type`.
- Output is capped at `max_chars` (default 32000) with a trailing
  `[truncated: N more chars]` note, and passes through the same redaction as
  every other tool.
- Loopback, RFC1918, link-local and unique-local addresses are refused, including
  redirect targets and names that resolve to them. Set `fetch.allow_private: true`
  to lift this.

```yaml
fetch:
  allow: [docs.example.com]   # suffix match; empty means any domain
  deny: [internal.example.com] # deny wins over allow
  allow_private: false
```

`fetch_url` is not granted by default. Give it to an agent with
`/agent tools <name> add fetch_url`, or ask groot to call `grant_tool`. See
[Granting tools](SPACES-AND-AGENTS.md#granting-tools).

Its output is untrusted content: it is wrapped and scanned like the other
external reads. See [Untrusted content](SAFETY.md#untrusted-content).

## MCP servers

Groot can call tools from [MCP](https://modelcontextprotocol.io) servers. Servers
are defined in the global `~/.groot/groot.yaml` only: a project `groot.yaml`
cannot start a process. A project file may narrow the set with `mcp.enable`, which
names the global servers its space uses. Without `enable`, every global server is
enabled.

<!-- mcp-example:start -->
```yaml
mcp:
  servers:
    grafana:
      transport: http
      url: https://grafana.internal/mcp
      headers:
        Authorization: "Bearer ${GRAFANA_TOKEN}"
      tools: [query_prometheus, search_dashboards, get_panel]
      read_only: [query_prometheus, search_dashboards, get_panel]
      timeout: 20s
    kubectl-mcp:
      transport: stdio
      command: kubectl-mcp
      args: ["--read-only"]
      env:
        LOG_LEVEL: "warn"
      tools: [list_pods, get_pod_logs]
      read_only: [list_pods, get_pod_logs]
```
<!-- mcp-example:end -->

- `transport: stdio` starts `command` with `args`. `transport: http` connects to a
  streamable HTTP endpoint at `url`. Plain `http://` is accepted only for localhost.
- `tools` is the allowlist. A server without `tools` is refused, and groot never
  offers a tool the list does not name.
- `read_only` marks the tools that only read. Work mode runs them without asking.
  Every other allowlisted tool counts as a write, and work mode asks before it runs.
- Tool names are `<server>__<tool>`, for example `grafana__query_prometheus`. Any
  character outside letters, digits, `_` and `-` becomes `_`, and the name is cut
  to 64 characters.
- Grant a tool with `/agent tools <agent> add grafana__query_prometheus`, or the
  whole allowlist with `grafana__*`. Groot can do the same with `grant_tool`.
- Trust: servers run outside the bash sandbox. A stdio server gets a clean
  environment (`PATH`, `HOME` and the `env` you list, nothing else) and the space
  directory as its working directory. Treat a server as trusted as the config
  that lists it. Its output is untrusted content, wrapped and scanned like web
  pages, and each result is capped at 64 KiB.
- Secrets go in `${VAR}`, which is read from the environment when the server
  connects and never written into the config. An unset variable is an error that
  names it.
- `timeout` bounds each call (default `30s`, at most `5m`). A server that fails to
  connect is retried after 10 seconds. Its conversations still open, and show a
  warning that names the server and the error.
- `/mcp` lists the servers of the active space with their state, and for a
  connected server each allowlisted tool with `ro` or `rw` and the agents that hold
  it. `/mcp connect <name>` connects one now, in the background: the transcript
  shows its result when the connect ends, and the UI keeps running meanwhile.
- MCP tools reach a conversation shortly after it opens, not at open. A slow server
  never delays the open: the conversation answers without its tools until a note
  `mcp <server>: N tools ready` appears, and the next turn offers them. A server
  that fails warns as before.
- `groot doctor` has an `mcp` section: one line per enabled server, `ok` with the
  tool count and any allowlisted tool the server does not offer, or `failed` with
  the error. A failed server makes doctor exit with 1.
