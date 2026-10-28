# Gemini provider: adapter, model catalog, switch every agent

Backlog item 7 (provider adapters), first provider after Anthropic. Opened 2026-10-28.
groot can call the Gemini Developer API with an API key. A model id decides the
provider, one catalog lists the models of every configured provider, and one
command moves every agent of a space to a model.

## Specs

S1. "A `gemini-` model id runs on Gemini; any `claude-` id runs on Anthropic. Any
  other id runs on `provider.name`."
S2. "The Gemini key follows the Anthropic key path: the environment variable, a
  `.env` file, the keychain item, the credentials file. Each provider has its own
  item and field. No second store."
S3. "The default provider must have a key to start. The other is optional: without
  its key, its models are not listed and a call to it fails with a clear message."
S4. "`/models` and `/model list` list the models of every provider with a key, each
  tagged with its provider. Prices come from the price table when known."
S5. "A conversation whose model moves to another provider gets a provider of that
  kind. Usage and cost stay summed per conversation."
S6. "`/model --all <id>` sets the model of every agent of the active space, saves it
  in each agent's file, switches the open conversations of those agents, and sets
  the space default. The id is checked against the merged catalog unless `--force`."
S7. "Cached input is priced per model: Gemini 4 Argon reads cache at 5% of its input
  price, Gemini 3.8 Flash at 10%, Anthropic at 10%."
S8. "`groot doctor` shows the Gemini key and whether the model list is reachable. It
  never blocks startup."
S9. "No test reaches the network."

## Tasks

T1. (S2) `internal/auth`: a `Service` type (Anthropic, Gemini) with its environment
  variable, keychain item and credentials field. The zero value is Anthropic, so
  existing callers keep working. `groot auth login|status|logout --provider gemini`.
T2. (S5, S1) `internal/provider/router.go`: `Router` implements `Provider`,
  `UsageReporter` and `ModelLister`. It keeps one provider per kind, builds a new
  one when a model of another kind is selected, sums usage, and merges the lists.
T3. (S1, S3) `cmd/groot/main.go`: `provider.name` accepts `anthropic` and `gemini`.
  The key required is the one of the configured model's provider.
T4. (S7) `internal/provider/pricing.go`: `CacheRead` per row. Gemini rows:
  `gemini-4-argon` 2.00 / 10.00 and `gemini-3.8-flash` 0.75 / 3.75, both
  introductory, with the later prices in comments.
T5. (S4) `internal/provider/gemini.go`: the adapter over `google.golang.org/genai`.
  Streaming text goes to `OnText`. Function calls and results map to function call
  and function response parts. A tool result gets its function name from the
  matching tool call in the same request. Thought signatures are replayed to Gemini
  only. Effort `low`, `medium`, `high` maps to a thinking level; `xhigh` and `max`
  clamp to `high`. Usage: prompt minus cached is input, candidates plus thoughts is
  output, cached is cache read.
T6. (S6) `internal/workspace`: `SetSpaceAgentsModel`. `internal/ui`: `/model --all`,
  its help line and its completion once a dash is typed.
T7. (S8) `cmd/groot/doctor.go`: a Gemini key line and a Gemini models line. The probe
  lists models with a 5 second budget and reports only the HTTP status or that the
  connection failed.
T8. `internal/provider/anthropic.go`: the message service is injected through
  `AnthropicOptions.Messages`, defaulting to the client it builds. Anthropic never
  replays a Gemini thought signature.

## Limits

- Gemini cache writes are not billed (implicit caching), so `CacheCreationTokens`
  stays zero for Gemini.
- Gemini's live API has not been exercised by the test suite. The tests use canned
  streams; the first real tool-calling turn is the acceptance check.
- A conversation that moves from Anthropic to Gemini in the middle of a tool loop may
  be rejected by Gemini if the earlier function calls carry no thought signature. A new
  conversation is not affected.
- The default model for `provider.name: gemini` is `gemini-3.8-flash`.

## Testing

- `internal/provider/gemini_test.go` drives the adapter through an injected
  `http.RoundTripper`. It covers a text turn, a tool-call turn, a tool-result replay
  with the recovered name, thought signature replay, usage accounting, the effort
  clamp, the finish reason mapping, 429, 401 and 403, a transport failure, and the
  model listing.
- `internal/provider/router_test.go` covers prefix routing, builds on switch, usage
  kept across switches, and the merged listing when one provider fails.
- `internal/ui/model_all_test.go` and `internal/workspace/model_all_test.go` cover
  `/model --all`: every agent changed, a refused id, `--force`, and the missing
  space.
- `cmd/groot/keys_test.go` covers the key the default provider needs.
