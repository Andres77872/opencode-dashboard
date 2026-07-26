# MiniMax analytics assistant

The analytics assistant is an optional, web-only report agent. It answers
questions about normalized usage from every source registered with the
dashboard, including OpenCode, Claude Code, Codex, Kimi Code, Qwen Code, and
sources added later.

It is intentionally not a coding agent. It cannot edit files, run commands,
query arbitrary SQL, call arbitrary URLs, read source configuration, or inspect
raw conversation transcripts.

## Availability and configuration

The assistant uses MiniMax's OpenAI-compatible HTTP API directly. No MiniMax or
OpenAI SDK is linked into the dashboard.

Set a MiniMax API key before starting the web dashboard:

```bash
export OPENCODE_DASHBOARD_MINIMAX_API_KEY='...'
opencode-dashboard web
```

As with quota reporting, the MiniMax coding-plan credential in OpenCode's local
auth store is used as a fallback when the environment variable is absent. The
credential remains in the Go backend and is never returned to the browser.

The international API base is `https://api.minimax.io/v1`. A server-side base
URL override, `OPENCODE_DASHBOARD_MINIMAX_BASE_URL`, is available for the
official China region and integration tests; it is never accepted from a
browser request.

One complete agent run — including every delegated specialist — is bounded to
90 seconds by default. Set `OPENCODE_DASHBOARD_MINIMAX_TIMEOUT` to a Go duration
from `10s` through `5m` when measured M3 latency requires a different local
limit.

The floating chat is rendered only after an authenticated `GET /v1/models`
response contains the exact, case-sensitive model ID `MiniMax-M3`. A missing
key, an account without M3 entitlement, or an availability-probe failure keeps
the assistant disabled. The dashboard never silently substitutes an older
model. Status probes are singleflight-cached for one minute when available and
briefly when unavailable; every chat still rechecks entitlement before sending
messages.

## Data that leaves the machine

Using the assistant changes the dashboard's otherwise local-only privacy model.
Each chat request can send the following to MiniMax:

- the messages typed into the assistant and its prior replies in that browser
  conversation;
- a short system policy defining the report-only role;
- tool schemas;
- the task text the lead agent writes when it delegates an investigation;
- the model, provider, tool, and pricing-snapshot names those metrics belong to,
  and each project's own name without its directories; and
- only the aggregate usage metrics requested by the model, such as distinct
  request/message counts, tokens, daily buckets, model totals, tool totals,
  request-coverage metadata, cost provenance, normalized source scan counts,
  and sanitized cache-health flags.

The assistant tools do **not** send raw prompts, assistant text, reasoning,
patches, tool inputs or outputs, configuration contents, secrets, filesystem
paths, session titles, raw diagnostics/errors, timestamps, request/session
identifiers, or message/session detail.

### Naming policy

A report that ranks `model-7f3a9b…` answers nothing, so identifiers that are
published product names travel exactly as recorded: model IDs, provider IDs,
coding-assistant tool names, and pricing snapshot IDs. There is deliberately no
fixed allowlist of known models — catalogs change weekly, and an unrecognized
new release would otherwise be reported as an unreadable pseudonym.

What is checked instead is *shape*. A value is reported verbatim only when it
looks like an identifier: at most 96 characters of letters, digits, and
`. _ : @ + - /`, with at most two slashes. Anything shaped like local state —
an absolute path, a home directory, a relative path, a URL, or a value
containing whitespace or control characters — is replaced with a 96-bit
process-scoped HMAC pseudonym instead.

Projects are reported by their leaf name only: `/home/you/work/alpha` is sent as
`alpha`, alongside a stable `project_ref` for correlation. A name that does not
survive that reduction is omitted entirely, leaving only the reference. Session
identities stay opaque in every case, because a session's title is the user's
own prompt text. Registered source IDs must use the server's restricted ID
syntax so future adapters remain selectable without exposing source labels.

## Agents

One question is answered by a lead analyst that may delegate bounded
investigations to specialists:

| Agent | Role | Tools |
|-------|------|-------|
| `analyst` (lead) | Answers the user and coordinates specialists | every analytics tool plus `delegate_to_specialist` |
| `trend_analyst` | How usage changed over time | `list_sources`, `get_overview`, `get_daily_usage`, `get_usage_trend_by_dimension` |
| `cost_auditor` | Spend, token efficiency, cost provenance | `list_sources`, `get_overview`, `get_model_usage`, `get_session_usage`, `get_cross_source_overview` |
| `tooling_analyst` | Tool adoption and failure patterns | `list_sources`, `get_tool_usage`, `get_usage_trend_by_dimension`, `get_overview` |
| `workload_analyst` | Project and session concentration | `list_sources`, `get_project_usage`, `get_session_usage`, `get_usage_trend_by_dimension`, `get_overview` |
| `integrity_auditor` | Source ingestion, accounting evidence, and cache freshness | `list_sources`, `get_source_integrity`, `get_overview` |

Specialist runs are deliberately isolated:

- a specialist receives only the task the lead wrote — never the conversation,
  the user's wording, or another specialist's work;
- a specialist cannot delegate, and its tool allowlist is enforced by the
  runner, not only by its prompt;
- each specialist has its own round and tool-call budget inside the shared
  per-question budget; and
- its finding returns to the lead as ordinary tool evidence, so the lead must
  reconcile it against the numbers it already holds.

At most three specialists may run for one question. Independent specialists run
concurrently, and their progress is streamed under the delegation that started
them.

## Backend agent loop

The browser makes one request to the local dashboard. All reasoning/tool rounds
run in the Go backend:

```text
web chat
   |
   | POST /api/v1/assistant/chat/stream (flushed NDJSON)
   v
lead agent loop ---- POST /v1/chat/completions, stream=true ----> MiniMax-M3
   ^          |                                   |
   |          | delegate_to_specialist            | tool_calls
   |          v                                   v
   |   specialist loop (own budget) ----> allowlisted analytics tools
   |                                              |
   +----------------------------------------------+---- source.Registry
```

For every model round the backend:

1. Sends the agent's system policy, its bounded history, and only the tool
   schemas that agent may call. Allowlist-validated navigation hints are
   attached to the current user turn as explicitly untrusted data; they never
   enter the system message.
2. If M3 returns tool calls, appends the complete assistant message—including
   its private interleaved-reasoning fields—to the server-side request history.
3. Validates every function name and JSON argument against the agent's
   allowlist and the shared budget.
4. Executes the round's read-only analytics tools concurrently through
   `source.Registry`, and starts any delegated specialist run.
5. Appends bounded JSON tool results, in the order the model asked for them, and
   asks M3 to continue.
6. Streams privacy-safe answer deltas, round boundaries, tool lifecycle, and
   specialist lifecycle to the web client as they happen. Provider reasoning has
   no browser event shape and is never returned or logged.
7. Finishes with the canonical assistant prose and backend HMAC signature, so
   a later stateless request cannot forge prior assistant-role content. The UI
   commits that signed message to history only after the terminal event.

### Recovering instead of failing

A model mistake the model can correct is returned to it as a failed tool
result rather than ending the run: an unavailable tool name, a repeated
identical call, an invalid delegation, a result too large for the remaining
budget, and an exhausted budget all produce `ok:false` envelopes with a code the
model can act on. Provider-supplied names outside the allowlist are never
echoed to the browser or the chat log.

When the round budget or the evidence budget is spent, the last round is sent
with **no tools at all** plus a notice telling the model to answer from the
evidence it already has and disclose what it could not verify. A bounded report
with stated limits is better than a loop-limit error.

Transient provider failures (HTTP 429, 5xx, and transport faults) are retried
once with jittered backoff, and anything already streamed for that round is
withdrawn from the browser first so a retry cannot show two interleaved
attempts. Authentication and model-availability failures are never retried.

The loop has hard limits for rounds per agent, total tool calls, delegations,
repeated identical calls, tool-result bytes, concurrent chats, request size, and
elapsed time. Browser cancellation propagates through the model call, the
specialist runs, and the source queries.

## Usage accounting

Every provider round is counted, and MiniMax's token counters are recorded when
it reports them: input, output, cached input, reasoning, and total tokens. A
turn's usage covers the lead agent and every specialist it started; a session's
usage is the sum of its turns. Counters are local telemetry about this
dashboard's own assistant — they never contain prompt or completion text.

A zero counter means the provider reported nothing, never that the work was
free: the UI shows "N model requests" instead of inventing a token total, in the
same spirit as the dashboard's treatment of usage-unavailable source requests.

## Available tools

| Tool | Intended use | Important behavior |
|------|--------------|--------------------|
| `list_sources` | Discover sources before selecting one | Dynamic registry metadata only; no source paths |
| `get_overview` | Totals for one explicit source and range | Sessions, outbound requests, transcript messages, days, tokens, source cost/provenance, and Kimi coverage when available |
| `get_source_integrity` | Audit one or all registered sources | Aggregate availability, source-wide ingestion, period accounting/cost evidence, and sanitized cache-window findings; no local identifiers or raw errors |
| `get_cross_source_overview` | Compare all available sources | Additive request/message/token totals are combined; dollar costs remain per source; omitted source dimensions are explicit |
| `get_daily_usage` | Find trends and spikes | Distinct request/message counts in a validated period/custom range and at most 1,000 daily/hourly buckets |
| `get_usage_trend_by_dimension` | Attribute a change to a model, tool, or project | Bounded daily/hourly series grouped by one dimension |
| `get_model_usage` | Compare models for one source | Bounded rows with outbound requests, tokens, sessions, and cost provenance; legacy `messages` remains additive compatibility data |
| `get_tool_usage` | Analyze tool adoption and failures | Bounded invocation/success/failure/session counts |
| `get_project_usage` | Analyze project concentration | Ranked projects with a stable reference and the project's leaf name; local IDs and paths omitted |
| `get_session_usage` | Rank coding sessions | Opaque session references, aggregate metrics, and the owning project's name |
| `delegate_to_specialist` | Hand off a focused investigation | Lead agent only; bounded specialist run returning a written finding |

Tools accept the dashboard's period presets (`1h`, `6h`, `12h`, `24h`, `72h`,
`1d`, `7d`, `14d`, `30d`, `1y`, and `all`) or validated ISO dates. A source
must be explicit for single-source tools; there is no implicit fallback inside
the agent. Time-series tools reject `all` and oversized custom/hourly windows
before source execution; all-time totals and rankings remain available through
the non-bucketed overview/model/tool/project tools. Cross-source payloads list
any unavailable source and any model/tool/project/trend dimension that failed,
so a partial ranking cannot silently look complete.

## Request and usage completeness

The assistant treats `requests` and `messages` as different contracts:

- `requests` counts outbound assistant/API attempts and excludes user prompts;
  it is authoritative for questions about API calls, retries, resends,
  compaction calls, failed attempts, or request volume;
- `messages` is retained for transcript/history semantics and can include user
  prompts; and
- model aggregates expose `requests` as the authoritative count and also
  retain a backward-compatible `messages` field for the same native
  assistant/API rows. The system policy tells the assistant to describe those
  rows as requests, not transcript messages.

Kimi aggregates can include `request_accounting`: `usage_recorded`,
`usage_recovered`, `usage_unavailable`, its fixed
`cancelled`/`interrupted`/`failed`/`unknown` reason partition, and
`trace_coverage`. Coverage values mean:

- `complete` — durable request traces cover the attempts in the selected data;
- `mixed` — observed request traces and requests inferred from legacy usage
  evidence are both present;
- `successful_only` — older usage-only logs reveal successful requests but
  cannot reveal failed attempts that were never persisted; and
- `unknown` — the available evidence cannot establish trace completeness.

The assistant must report usage-unavailable requests when material. Their
request count is known, but their tokens and cost are unknown, never zero.
The reason partition classifies persisted evidence only: cancelled requests
have a durable cancellation, failed requests were superseded by a same-step
retry, interrupted requests were still open when the persisted log ended, and
unknown requests have no stronger evidence. These categories may still be
billable. Complete trace coverage only means attempts were traced; it does not
mean usage or cost evidence is complete.
Kimi Code versions before 0.23.1 can lack durable `llm.request` traces, so one
successful request is inferred per standalone usage record without inventing
missing failures. `step.end.usage` can recover token evidence when the canonical
`usage.record` is absent; that provenance remains explicit.

Kimi does not report a separate reasoning-token counter. Its generated-token
value remains in `tokens.output`, and neither the dashboard nor the assistant
creates a synthetic reasoning estimate.

## Cost semantics

Cross-source cost must never be presented as one spend total:

- OpenCode records reported spend.
- Codex is an estimated API-equivalent value, not subscription spend.
- Claude Code can mix reported and snapshot-computed values.
- Kimi Code is estimated from the dashboard's pinned official Kimi API pricing
  snapshot for requests with usage evidence; it is not membership or coding-plan
  spend.
- Qwen Code is estimated from the pinned Alibaba Cloud API list-price snapshot;
  it is not coding-plan or Token Plan spend.

The agents receive and must retain each source's `cost_status` and
`cost_provenance`, and must name the model, provider, tool, or project a figure
belongs to rather than describing it in the abstract. Cross-source rankings use cost-neutral signals such as tokens
or invocation counts. Reports state the requested period, included/unavailable
sources, and material data limitations. When a run uses cross-source cost
context — including through a specialist — the backend appends a deterministic
source-scope notice even if model prose omits it.

Kimi subscription quota is a separate live signal. The dashboard obtains it
from the same managed `/usages` surface used by Kimi Code's official `/usage`
command and follows the official OAuth refresh/lock flow; it is not inferred
from estimated transcript cost. A transient failure can leave a stale
last-good quota visible, clearly marked stale.

## Saved conversations

Completed turns are persisted to a dedicated SQLite database
(`$XDG_DATA_HOME/opencode-dashboard/assistant-chat.sqlite`), separate from the
rebuildable usage cache. Each turn stores the prompt and the signed answer plus
everything that produced it: every analytics tool call with its exact input
arguments and result envelope, every specialist run with its task, finding,
status, rounds and usage, the turn's provider token accounting and duration, the
dashboard view the question was asked from, and any deterministic notice the
backend appended.

Restoring a conversation therefore reproduces exactly what was shown live,
including the specialist cards, the nested tool calls a specialist made, and the
per-turn cost line. Persisting the assistant-history signing key alongside the
conversations keeps restored answers replayable as signed history after a
restart.

Assistant history is **not migrated**. The store is a local convenience rather
than a system of record, so a database written by a different schema version is
rebuilt empty and the reset is logged at startup. A database that is not an
assistant chat store is never modified.

## HTTP surface and security

The web server exposes:

- `GET /api/v1/assistant/status` — runtime M3/key availability, privacy notice,
  consent version, capabilities, the delegable specialist roster, and whether
  sessions are persisted.
- `POST /api/v1/assistant/chat` — a bounded, stateless user/assistant history and
  optional UI context hints. It must echo the currently accepted consent
  version, and prior assistant turns must carry valid backend signatures.
  Analytics tools remain authoritative.
- `POST /api/v1/assistant/chat/stream` — the same signed agent loop as a flushed
  `application/x-ndjson` response. Events are `start`, `round_start`,
  `content_delta`, `content_reset`, `tool_start`, `tool_finish`,
  `subagent_start`, `subagent_finish`, and terminal `complete` or `error`. Tool
  events expose a stream-local call ID, the allowlisted name, the validated
  arguments, the safe result envelope, the agent that ran it, and the delegation
  it belongs to. The terminal `complete` event carries the signed message, the
  turn's usage and timing, the canonical tool and specialist records, and the
  session the turn was appended to.
- `GET /api/v1/assistant/sessions`, `GET /api/v1/assistant/sessions/{id}`, and
  `DELETE /api/v1/assistant/sessions/{id}` — list, restore, and delete saved
  conversations. Deleting a session removes its turns, messages, tool calls, and
  specialist runs.

All assistant responses use `Cache-Control: no-store`. Assistant requests
accept the exact dashboard origin, plus the checked-in Vite development origin
on port 7451; unrelated loopback origins and cross-site Fetch Metadata are
rejected.
Chat accepts JSON only and never accepts a provider URL, API key, tool
definition, agent definition, or executable operation from the client. The
server continues to bind to loopback by default.
