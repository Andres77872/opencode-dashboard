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

One complete agent run is bounded to 60 seconds by default. Set
`OPENCODE_DASHBOARD_MINIMAX_TIMEOUT` to a Go duration from `10s` through `2m`
when measured M3 latency requires a different local limit.

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
- tool schemas; and
- only the aggregate usage metrics requested by the model, such as distinct
  request/message counts, tokens, daily buckets, model totals, tool totals,
  request-coverage metadata, and cost provenance.

The assistant tools do **not** send raw prompts, assistant text, reasoning,
patches, tool inputs or outputs, configuration contents, secrets, filesystem
paths, session titles, or message/session detail. Chat history is kept in the
browser for the open panel only and is not added to the dashboard SQLite cache.

Project analytics are treated conservatively because project names and IDs can
contain local directory information. Externally supplied tool results use
anonymous ranks and 96-bit process-scoped HMAC references with aggregate
metrics instead of local names or paths. Known public model, provider, and tool
identifiers use a conservative policy: model IDs and pricing snapshot IDs are
always replaced with process-scoped pseudonyms; only provider and tool names on
strict public allowlists may remain readable. Unknown values are also
pseudonymized before leaving the backend. Registered source IDs must use the
server's restricted ID syntax so future adapters remain selectable without
exposing source labels.

## Backend agent loop

The browser makes one request to the local dashboard. All reasoning/tool rounds
run in the Go backend:

```text
web chat
   |
   | POST /api/v1/assistant/chat/stream (flushed NDJSON)
   v
bounded agent loop ---- POST /v1/chat/completions, stream=true ----> MiniMax-M3
   ^                                              |
   |                                              | tool_calls
   |                                              v
allowlisted analytics tools <---- validated name + JSON arguments
   |
   +---- source.Registry ---- live or cached normalized sources
```

For every model round the backend:

1. Sends the system policy, bounded user/assistant history, and tool schemas.
   Allowlist-validated navigation hints are attached to the current user turn
   as explicitly untrusted data; they never enter the system message.
2. If M3 returns tool calls, appends the complete assistant message—including
   its private interleaved-reasoning fields—to the server-side request history.
3. Validates each function name and JSON argument against the allowlist.
4. Executes read-only aggregate tools sequentially through `source.Registry`.
5. Appends bounded JSON tool results and asks M3 to continue.
6. Streams privacy-safe answer deltas and tool start/finish events to the web
   client as they happen. Tool arguments, results, and provider reasoning have
   no browser event shape and are never returned or logged.
7. Finishes with the canonical assistant prose and backend HMAC signature, so
   a later stateless request cannot forge prior assistant-role content. The UI
   commits that signed message to history only after the terminal event.

The loop has hard limits for rounds, total tool calls, repeated identical calls,
tool-result bytes, concurrent chats, request size, and elapsed time. Browser
cancellation propagates through the model call and source queries.

## Available tools

| Tool | Intended use | Important behavior |
|------|--------------|--------------------|
| `list_sources` | Discover sources before selecting one | Dynamic registry metadata only; no source paths |
| `get_overview` | Totals for one explicit source and range | Sessions, outbound requests, transcript messages, days, tokens, source cost/provenance, and Kimi coverage when available |
| `get_cross_source_overview` | Compare all available sources | Additive request/message/token totals are combined; dollar costs remain per source; omitted source dimensions are explicit |
| `get_daily_usage` | Find trends and spikes | Distinct request/message counts in a validated period/custom range and at most 1,000 daily/hourly buckets |
| `get_model_usage` | Compare models for one source | Bounded rows with outbound requests, tokens, sessions, and cost provenance; legacy `messages` remains additive compatibility data |
| `get_tool_usage` | Analyze tool adoption and failures | Bounded invocation/success/failure/session counts |
| `get_project_usage` | Analyze project concentration | Anonymous ranked projects with process-scoped references; local names, IDs, and paths omitted |

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
`usage_recovered`, `usage_unavailable`, and `trace_coverage`. Coverage values
mean:

- `complete` — durable request traces cover the attempts in the selected data;
- `mixed` — observed request traces and requests inferred from legacy usage
  evidence are both present;
- `successful_only` — older usage-only logs reveal successful requests but
  cannot reveal failed attempts that were never persisted; and
- `unknown` — the available evidence cannot establish trace completeness.

The assistant must report usage-unavailable requests when material. Their
request count is known, but their tokens and cost are unknown, never zero.
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

The agent receives and must retain each source's `cost_status` and
`cost_provenance`. Cross-source rankings use cost-neutral signals such as tokens
or invocation counts. Reports state the requested period, included/unavailable
sources, and material data limitations. When a run uses cross-source cost
context, the backend appends a deterministic source-scope notice even if model
prose omits it.

Kimi subscription quota is a separate live signal. The dashboard obtains it
from the same managed `/usages` surface used by Kimi Code's official `/usage`
command and follows the official OAuth refresh/lock flow; it is not inferred
from estimated transcript cost. A transient failure can leave a stale
last-good quota visible, clearly marked stale.

## HTTP surface and security

The web server exposes:

- `GET /api/v1/assistant/status` — runtime M3/key availability, privacy notice,
  and consent version used by the web UI.
- `POST /api/v1/assistant/chat` — a bounded, stateless user/assistant history and
  optional UI context hints. It must echo the currently accepted consent
  version, and prior assistant turns must carry valid backend signatures.
  Analytics tools remain authoritative.
- `POST /api/v1/assistant/chat/stream` — the same signed agent loop as a flushed
  `application/x-ndjson` response. Events are `start`, `content_delta`,
  `content_reset`, `tool_start`, `tool_finish`, and terminal `complete` or
  `error`. Tool events expose only a stream-local call ID, allowlisted name, and
  completion status; arguments and aggregate results stay server-side.

All assistant responses use `Cache-Control: no-store`. Assistant requests
accept the exact dashboard origin, plus the checked-in Vite development origin
on port 7451; unrelated loopback origins and cross-site Fetch Metadata are
rejected.
Chat accepts JSON only and never accepts a provider URL, API key, tool
definition, or executable operation from the client. The server continues to
bind to loopback by default.
