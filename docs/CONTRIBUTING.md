# Contributing

Development commands, end-to-end tests, and how to add a tool or a provider.

## Adding a tool

1. Write a type that implements `tool.Tool`: `Definition`, `Risk`, `Preview`,
   `Execute`. Put it in `internal/tool/`.
2. Add a test for the path or argument rules and for the preview.
3. Register it in `cmd/groot/main.go`.

Choose the risk honestly. `RiskRead` runs without asking, so anything that
changes state must be `RiskWrite` or `RiskExec`.

## Adding a provider

Implement `provider.Provider` (`Send`, `Model`, `SetModel`) in
`internal/provider/`. Keep the SDK types inside the adapter and convert to and
from `api.Message` and `api.Response`. Add a `KindRateLimit`-style mapping for
typed errors, and add the pricing row if you want cost estimates.

## Development

```sh
make test     # go test ./... -count=1
make lint     # go vet and a gofmt check
```

`internal/arch` fails the build if a core package imports an SDK, the UI, or
`os/exec` outside `tool/exec.go`.

### End-to-end tests

The `e2e/` package calls the real Anthropic API through the workspace layer: one
text turn (usage, cost and context are recorded), a turn where the model runs
`read_file` on a file in the space, resuming a session in a second instance, and
an agent budget that refuses a turn without a model call.

```sh
ANTHROPIC_API_KEY=... make e2e   # go test -tags e2e ./e2e/... -count=1 -v
```

- Without `ANTHROPIC_API_KEY` in the environment every test skips with a message.
  The package never reads a `.env` file.
- Cost: claude-haiku-5-5 with tiny prompts and `max_tokens` 256. A run should
  stay under about $0.05; the last test prints the total spend.
- CI does not run them. Only `make e2e` does, on a machine with a key.
