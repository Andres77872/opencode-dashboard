# opencode-dashboard

> Local analytics for your AI coding-assistant usage — one binary, web or terminal, works offline by default.

See your usage across **OpenCode**, **Claude Code**, **Codex**, **Kimi Code**, and **Qwen Code** — sessions, outbound requests, costs, tokens, models, tools, projects, and transcript messages — through a web dashboard or a terminal UI. Source files are never modified, and normal dashboard views remain local. The optional web assistant is the explicit exception: when used, it sends the chat and requested aggregate metrics to the provider destination selected on the Config page.

## Overview

**opencode-dashboard** reads each tool's local usage data read-only, stores dashboard-owned aggregate metadata in a local SQLite cache, and renders it two ways:

- **Web dashboard** — browser SPA served at `http://127.0.0.1:7450`
- **TUI dashboard** — terminal interface built with Bubble Tea

It supports five data sources, all read **read-only** and **local-only**:

| Source | ID | Storage | Default location |
|--------|----|---------|------------------|
| OpenCode | `opencode` | SQLite database | channel DBs under `~/.local/share/opencode/` |
| Claude Code | `claude_code` | JSONL transcripts | `~/.claude` |
| Codex | `codex` | JSONL transcripts | `~/.codex` |
| Kimi Code | `kimi_code` | Session state + agent wire JSONL | `~/.kimi-code` |
| Qwen Code | `qwen_code` | Chat transcripts + token-usage JSONL | `~/.qwen` |

Most views are scoped to one selected source. The **Overview** is the exception: it merges every available source into combined totals plus a per-source breakdown. You can switch the active source and time range live in both interfaces. No OpenCode (or other) server needs to be running, and at least one source's local data is all that's required. The dashboard creates `~/.local/share/opencode-dashboard/usage-cache.sqlite` by default; an empty cache is consolidated by a background sync at startup while views are served live from raw data. Once a source is cached, hours older than the finality cutoff (six hours behind now, truncated to the hour) are served from the cache, and the window after the cutoff is always read live from raw content and merged into the result — so recent activity is never missing, whether or not the source changed. Consolidation catches up on its own: a background sync runs every 30 minutes, and a read also starts one when the last successful sync is older than seven hours. The web UI additionally offers an explicit incremental resync and a clear-and-rebuild action.

## Data sources

Each source is detected automatically and exposed with its own capabilities, diagnostics, cost policy, and privacy posture. A source that is missing or unreadable is reported as *unavailable* rather than failing the whole dashboard.

| Source | Kind | Resolution order | Cost provenance |
|--------|------|------------------|-----------------|
| OpenCode | `sqlite` | `--db` → `--channel` → `OPENCODE_DASHBOARD_DB` → auto-detect (stable → latest → beta) | `reported` — real spend recorded by OpenCode |
| Claude Code | `jsonl` | `--claude-home` → `CLAUDE_CONFIG_DIR` → `~/.claude` | `mixed` — reported when present, else computed from a bundled pricing snapshot, else missing |
| Codex | `jsonl` | `--codex-home` → `OPENCODE_DASHBOARD_CODEX_HOME` → `~/.codex` | `estimated_api_equivalent` — estimated USD from official API per-token rates, **not** actual billed spend |
| Kimi Code | `jsonl` | `--kimi-home` → `KIMI_CODE_HOME` → `~/.kimi-code` | `estimated_api_equivalent` — estimated from official Kimi API prices, **not** actual membership or coding-plan spend |
| Qwen Code | `jsonl` | `--qwen-home` → `QWEN_CODE_HOME` → `~/.qwen` | `estimated_api_equivalent` — estimated from Alibaba Cloud Model Studio list prices, **not** actual coding-plan or Token Plan spend; unpriced models stay `missing` |

### Cross-source costs

The Overview deliberately does **not** present a single combined cost number. OpenCode reports real dollars, Codex, Kimi Code, and Qwen Code report estimated API-equivalent values, and Claude Code is mixed — summing them would be misleading. Costs are always shown per source with each source's own provenance, while additive metrics (sessions, requests, messages, tokens, days) are combined. A **request** is an outbound assistant/API attempt and excludes user prompts; **messages** remain the transcript/history count. Cross-source "top" signals (models, projects, tools) are ranked by a cost-neutral metric (tokens / invocations) so real and estimated dollars are never compared.

### Codex requested processing mode

For recent Codex CLI rollouts, the dashboard reads each persisted `thread_settings_applied.thread_settings.service_tier` event and attaches that setting to the assistant API requests that follow it. Message rows and session details show **Fast requested**, **Standard requested**, **Flex requested**, or **Tier unknown**. The Codex Daily view can group estimated USD cost and exact input, cache-read, output, and reasoning token totals by this requested mode; the equivalent API query is `GET /api/v1/daily?source=codex&period=all&dimension=processing_mode`.

The classification is deliberately conservative:

- Codex wire values `priority` and `fast` map to **Fast requested**; `default` and `standard` map to **Standard requested**; `flex` maps to **Flex requested**.
- Missing or unrecognized markers remain **Tier unknown**. Older Codex CLI histories usually have no persisted marker, so the dashboard does not backfill them from the current `config.toml` value.
- The marker records what the local client requested. Codex rollouts do not persist the service tier returned by the backend, so this is not proof of the tier actually served or billed.
- Processing-mode totals use the same per-request cumulative-delta and fork/replay deduplication as the rest of Codex accounting. They do not sum overlapping `last_token_usage` records or copied histories.

All Codex costs stay in USD and use the official [OpenAI API per-token pricing catalog](https://developers.openai.com/api/docs/pricing). For these API-equivalent estimates, **Fast requested** uses Priority processing rates, **Flex requested** uses Flex processing rates, and **Standard requested** uses Standard rates. **Tier unknown** remains unknown for classification and falls back to Standard rates for the estimate only; that fallback does not mean Standard was requested, served, or billed. The locally persisted marker is never treated as server confirmation, so these values are estimates rather than actual billed spend.

This distinction matters because OpenAI's [Priority processing guide](https://developers.openai.com/api/docs/guides/priority-processing) says the response `service_tier` identifies the tier actually used and that a Priority request can be downgraded to Standard. Codex's local rollouts do not retain that response field. OpenAI's [Flex processing guide](https://developers.openai.com/api/docs/guides/flex-processing) documents Flex as the lower-cost, slower processing tier; its token rates follow the Flex column in the official pricing catalog.

### Kimi Code wire accounting

Kimi Code sessions are read from `sessions/<workspace>/<session>/state.json`. Both layouts are supported: v1 state documents with string or epoch timestamps and fields such as `workDir`, and the [v2 session metadata schema](https://github.com/MoonshotAI/kimi-code/blob/main/packages/agent-core-v2/src/session/sessionMetadata/sessionMetadata.ts) with `version`, `cwd`, title, fork, parent/swarm, labels, and agent metadata. When a state document lacks a usable directory, the adapter falls back through `cwd`, `workDir`, `custom.cwd`, and the workspace entry in `session_index.jsonl`.

`agents/main/wire.jsonl` is authoritative for the main conversation, and every other `agents/<agent>/wire.jsonl` is additive. Foreground, background, nested, and `independent` agents all contribute to one combined session/overview total; there is intentionally no main-versus-subagent split. A root `wire.jsonl` is used only when no main wire exists and it contains canonical agent records. Old UI-only root logs and root logs shadowed by a main wire are diagnosed but do not fabricate usage.

The adapter:

- counts visible user-origin prompts as transcript messages and every `llm.request` as its own outbound request, including normal calls, retries, resends, compaction calls, failures, and unfinished attempts;
- uses `usage.record` as canonical token evidence and can recover a missing record from `step.end.usage`; a later canonical record replaces the recovered value;
- treats consecutive standalone usage records as separate legacy successful requests instead of overwriting them;
- maps `inputOther`, `inputCacheRead`, `inputCacheCreation`, and `output` into the dashboard's disjoint token buckets;
- pairs `tool.call` and `tool.result` by `toolCallId`, preserves genuine user-slash skill/plugin prompts, and excludes system-triggered subagent tasks, background steering, injections, and model-triggered activation;
- attributes agent display metadata from the wire profile while rolling every agent type into the parent session; and
- starts accounting after the last durable `forked` marker, so copied parent history is not counted again in forked sessions.

Kimi Code releases before 0.23.1 can contain usage records without durable `llm.request` traces. For those logs the dashboard infers one successful request per standalone usage record and marks trace coverage `successful_only` or `mixed`; failed attempts that Kimi never persisted cannot be reconstructed. For traced requests without usage evidence, request count is known but tokens and cost are **unknown, never zero**. Aggregates expose `usage_recorded`, `usage_recovered`, `usage_unavailable`, the fixed `cancelled`/`interrupted`/`failed`/`unknown` reason partition, and trace coverage. Request detail exposes the same observed/inferred trace, recorded/recovered/unavailable usage provenance, and the strongest persisted reason for unavailable usage. “Interrupted” means the persisted log ended with the request open; it does not prove a crash, and none of the reasons is a billing verdict.

Kimi does not persist a separate reasoning-token counter. Its reported generated-token value remains in `tokens.output`; the dashboard does not synthesize a reasoning estimate.

### Kimi model pricing catalog

The bundled snapshot (`kimi-api-pricing-2026-07-16`) uses Kimi's official per-million-token API prices, in USD. Cache creation is priced as a cache miss because the public tables expose cache-hit and cache-miss input rates. These values are an API-equivalent estimate for requests with persisted usage evidence, including usage recovered from `step.end`; they are not actual membership/coding-plan spend. Requests without usage evidence have unknown cost and are never priced as zero.

| Canonical API model | Context | Cache hit | Input cache miss | Output |
|---------------------|---------|-----------|------------------|--------|
| `kimi-k2.5` | 256K | $0.10 | $0.60 | $3.00 |
| `kimi-k2.6` | 256K | $0.16 | $0.95 | $4.00 |
| `kimi-k2.7-code` | 256K | $0.19 | $0.95 | $4.00 |
| `kimi-k2.7-code-highspeed` | 256K | $0.38 | $1.90 | $8.00 |
| `kimi-k3` | 1M | $0.30 | $3.00 | $15.00 |

Managed Kimi Code aliases follow the current official model table: `kimi-code/k3` and `k3` → `kimi-k3`; `kimi-code/kimi-for-coding` → `kimi-k2.7-code`; and `kimi-code/kimi-for-coding-highspeed` → `kimi-k2.7-code-highspeed`. Historical K2.5 and K2.6 aliases are also retained so old transcripts remain priceable. Unknown or custom aliases stay `missing` rather than receiving a guessed price.

Sources: [Kimi Code model configuration](https://www.kimi.com/code/docs/en/kimi-code/models.html), [Kimi K2.5 pricing](https://platform.kimi.ai/docs/pricing/chat-k25), [Kimi K2.6 pricing](https://platform.kimi.ai/docs/pricing/chat-k26), [Kimi K2.7 Code pricing](https://platform.kimi.ai/docs/pricing/chat-k27-code), and [Kimi K3 pricing](https://platform.kimi.ai/docs/pricing/chat-k3).

### Qwen Code accounting

Qwen Code (the [qwen-code CLI](https://github.com/QwenLM/qwen-code)) records the same API request in up to three local stores, and the adapter reconciles them so every request is counted exactly once:

- **Chat transcripts** (`projects/<sanitized-cwd>/chats/<session>.jsonl`) are the backbone: user prompts, assistant messages with per-request `usageMetadata`, reasoning parts, and `functionCall`/`tool_result` pairs matched by call ID.
- **Telemetry echoes** (`system`/`ui_telemetry` `api_response` events in the same transcript) duplicate the assistant records' token counts; events that match an assistant record are folded away, and the remainder — auxiliary requests only telemetry saw, such as the managed memory-extractor subagent — become their own request rows with agent attribution.
- **The token-usage log** (`usage/token-usage-YYYY-MM.jsonl`, one line per successful API call since qwen-code v0.19.0) fills whatever the transcripts missed, including whole sessions with no transcript; `usage_record.jsonl` supplies the project path for those synthesized sessions.

Token counters overlap in the raw data (`cachedTokens ⊆ inputTokens` always; `thoughtsTokens ⊆ outputTokens` on the OpenAI-compatible `openai`/`qwen-oauth` auth paths, additive on Gemini-native auth). The adapter maps them into the dashboard's disjoint buckets — uncached input, cache reads, output excluding reasoning, and reasoning — using the auth type recorded next to each request. Failed API calls carry no token usage and are not counted as requests.

### Qwen model pricing catalog

The bundled snapshot (`qwen-modelstudio-pricing-2026-08-02`) uses Alibaba Cloud Model Studio international list prices, in USD per million tokens, with promotional discounts not applied. Tiered-context models use their base tier; longer contexts are billed higher upstream, so those estimates are conservative lower bounds.

| Model | Cache hit | Cache write | Input | Output | Base tier |
|-------|-----------|-------------|-------|--------|-----------|
| `qwen3.8-max` | $0.25 | $2.50 | $2.00 | $6.00 | 256K |
| `qwen3.8-max-preview` | $0.25 | $2.50 | $2.00 | $6.00 | 256K; priced identically to `qwen3.8-max` |
| `qwen3.7-max` | $0.25 | — | $2.50 | $7.50 | 256K |
| `qwen3.7-plus` | $0.04 | — | $0.40 | $1.60 | non-thinking 0–256K |
| `qwen3-coder-plus` | $0.10 | — | $1.00 | $5.00 | 0–32K; cache hit estimated at 10% of input |
| `qwen3-max` | $0.24 | — | $1.20 | $6.00 | 0–32K; cache hit estimated at 20% of input |

Two cache-hit rates are marked estimated because Model Studio does not publish a separate cache-hit price for those models; the input and output rates are published values.

A `—` in the cache-write column means the listing publishes no separate cache-write price, so writes bill at that model's input rate. Qwen's usage log reports no cache-write counter, so the rate only takes effect for a proxied model mapped into this catalog by a [pricing alias](#pricing-aliases), or when another source borrows a Qwen rate the same way.

A model outside this catalog — unknown or custom-endpoint — is reported as `missing` rather than guessed. Aliases map `coder-model` (qwen-oauth) to the current mainline coder model `qwen3.7-max`, `qwen-max` to `qwen3-max`, and `qwen-coder-plus` to `qwen3-coder-plus`.

Sources: [Model Studio pricing](https://www.alibabacloud.com/help/en/model-studio/model-pricing), [qwen-code token usage service](https://github.com/QwenLM/qwen-code/blob/main/packages/core/src/services/tokenUsageService.ts).

### Bundled pricing snapshots

Computed and estimated costs come from pinned, dated catalogs compiled into the binary — never a live pricing lookup — so a given transcript prices identically on every machine running the same release. Each snapshot carries its ID and retrieval date, and the API reports them alongside the rates:

| Source | Snapshot ID | Retrieved | Models |
|--------|-------------|-----------|--------|
| Claude Code | `anthropic-bundled-2026-07-24` | 2026-07-24 | 21 |
| Codex | `openai-codex-api-pricing-2026-07-27` | 2026-07-27 | 14 |
| Kimi Code | `kimi-api-pricing-2026-07-16` | 2026-07-16 | 5 |
| Qwen Code | `qwen-modelstudio-pricing-2026-08-02` | 2026-08-02 | 6 |

OpenCode has no snapshot: it records real spend, which is read as reported. A model outside its source's snapshot stays `missing` rather than being priced by a guess — see [pricing aliases](#pricing-aliases) to map one by hand.

### Pricing aliases

A bundled catalog can only price models it knows. Proxied endpoints, renamed
deployments, and brand-new releases show up as `missing` rather than being
guessed. The web **Config** surface lets you resolve those by hand: every
provider/model pair a source has actually observed is listed with its current
pricing resolution, and you can point one at an exact catalog model.

- Aliases may target **any** source's bundled catalog, not just the selected
  source's own — a CLI often reports a model another vendor prices, and only
  that vendor's catalog has the right rates.
- A model that already prices natively is still aliasable. Name-based matching
  guesses; you are the authority on what a proxied model really is, so a user
  alias outranks native pricing.
- A target must be an exact catalog model with positive input and output rates,
  and both catalogs must price in the same currency — rates are per-million
  values in their own currency, so borrowing across currencies would silently
  mix units.
- An alias cannot point a model at itself, and a source whose own catalog failed
  to load is refused (a broken snapshot, not a mapping problem).

Aliases are user-authored, so they live in `dashboard-settings.sqlite` rather
than the rebuildable usage cache and survive a clear-and-rebuild. Changing one
changes the source's pricing identity, which starts (or queues, if a sync is
already running) a historical recollection so old and newly aliased costs are
never mixed in the same view. Each alias is reported back with its state:
`active`, `not_detected` (no matching observed model), `target_missing` (the
catalog entry is gone), or `ineffective`.

The equivalent API is `GET`/`POST`/`DELETE /api/v1/pricing/aliases`.

### Privacy

- **Read-only source history** — no transcript, session file, source database, or source configuration is ever written to or mutated. The Kimi quota monitor may refresh Kimi's OAuth credential file using the same atomic flow and cross-process lock as Kimi Code itself; it does not alter session history.
- **Local by default** — historical dashboard data is read from local paths and served on `127.0.0.1`. The quota monitor makes authenticated requests only for providers whose quota is exposed through an official live API (Kimi Code and MiniMax), and the optional analytics assistant sends the disclosed chat and aggregate metrics only to the globally selected provider destination when used.
- **Dashboard-owned local state** — everything the dashboard writes lives under `~/.local/share/opencode-dashboard/` and is removed by `opencode-dashboard uninstall` (listed below).
- **Self-maintaining consolidation** — an empty cache is built by a background sync at startup (views serve live raw data meanwhile). Once ready, the cache covers only hours older than the finality cutoff; the window after it is read live from raw content on every read and merged, so cached views stay complete through now. A background sync also runs every 30 minutes, and a read starts one when the last successful sync is stale. The web top bar database action opens a sync panel with status, progress, last update, logs, incremental resync, and clear-and-rebuild.
- **No cached transcripts** — raw conversation text, reasoning text, tool input, tool output, and patches are not stored in the dashboard cache.
- **Plaintext transcripts** — Claude Code, Codex, Kimi Code, and Qwen Code JSONL data is local plaintext and may contain prompts, reasoning, tool output, file paths, patches, and secrets.
- **Redaction** — config previews (`/api/v1/config`) redact obvious secrets before display.

#### Dashboard-owned files

| File | Contents | Rebuildable |
|------|----------|-------------|
| `usage-cache.sqlite` | Aggregate usage metadata; override with `--cache-db` or `OPENCODE_DASHBOARD_CACHE_DB` | Yes — from the sources |
| `dashboard-settings.sqlite` | Pricing aliases plus assistant provider/model settings and custom-provider API keys (plaintext, local file mode `0600`) | No — survives cache rebuilds |
| `assistant-chat.sqlite` | Saved analytics-assistant conversations (web assistant only) | No — and never migrated: a database from another schema version is rebuilt empty and the reset is logged |
| `claude-rate-limits.json` | Latest Claude Pro/Max quota snapshot from the statusline command | Yes — on the next Claude Code response |

## Quota tracking

Besides historical usage, the dashboard shows the **remaining subscription quota** for the provider accounts on the machine — a compact strip in the sidebar and detailed cards on the Overview page, served by `GET /api/v1/quotas`. Provider enforcement windows are normalized to used-percent with reset times; Kimi Code also exposes its optional Extra Usage balance and monthly spending cap. Collection uses **official surfaces only** — no reverse-engineered private endpoints:

| Provider | Setup | How the data is obtained |
|----------|-------|--------------------------|
| Codex | automatic | The Codex CLI records `rate_limits` snapshots in its own rollout files under `~/.codex/sessions`; the dashboard reads the newest one. No network, no credentials. |
| Claude Code | one command (below) | Claude Code's [documented statusline integration](https://code.claude.com/docs/en/statusline) pipes JSON including `rate_limits` to a configured statusline command. Pro/Max plans only. |
| Kimi Code | automatic after `kimi login` | The same managed `/usages` surface used by Kimi Code's official [`/usage` command](https://www.kimi.com/code/docs/en/kimi-code-cli/reference/slash-commands.html) is called with the OAuth credential under `$KIMI_CODE_HOME/credentials/`. Short-lived access tokens are refreshed through Kimi's official OAuth flow, using Kimi Code's `oauth/<credential>.lock` protocol to avoid refresh-token races with a running CLI. |
| MiniMax | API key | MiniMax's documented `token_plan/remains` endpoint, called with the key from `OPENCODE_DASHBOARD_MINIMAX_API_KEY` or, as a fallback, opencode's auth store (`~/.local/share/opencode/auth.json`, entry `minimax-coding-plan`). |

The Kimi collector follows the official open-source [managed usage client](https://github.com/MoonshotAI/kimi-code/blob/main/packages/oauth/src/managed-usage.ts) and [OAuth refresh manager](https://github.com/MoonshotAI/kimi-code/blob/main/packages/oauth/src/oauth-manager.ts), including scoped credentials for custom `KIMI_CODE_BASE_URL` / OAuth environments. Usage calls use an 8-second request timeout; refresh and cross-process locking share a 30-second budget. Refresh retries transient transport, 429, and selected 5xx failures with bounded backoff, re-reads credentials after peer rotation, and atomically tombstones only a confirmed `invalid_grant` revocation. Redirects remain blocked and a stale last-good quota stays visible after transient failure.

### Claude setup on a new machine

With the binary on `PATH` and Claude Code signed in (Pro/Max), run once:

```bash
opencode-dashboard claude-statusline --install
```

This writes the statusline entry into `settings.json` inside your Claude Code
config directory (`~/.claude` unless `CLAUDE_CONFIG_DIR` says otherwise),
preserving your other settings:

```json
{ "statusLine": { "type": "command", "command": "opencode-dashboard claude-statusline" } }
```

If a different statusline is already configured, the command refuses; re-run with `--force` to replace it. Then use Claude Code once — after the first response of a session, Claude Code starts invoking the command, which records the quota snapshot for the dashboard **and** renders a useful statusline (e.g. `Fable 5 · 5h 33% · wk 31%`).

### Freshness

Codex and Claude quota only refresh while their CLI is running; the dashboard marks snapshots older than 15 minutes with a *stale* badge instead of hiding them. Kimi Code and MiniMax are fetched live and cached for 60 seconds; after a transient API failure, the last successful value remains visible with a *stale* badge. The Claude snapshot lives at `~/.local/share/opencode-dashboard/claude-rate-limits.json` — dashboard-owned, removed by `opencode-dashboard uninstall`. `opencode-dashboard web` prints a one-line hint at startup if Claude quota tracking is not set up yet.

## Analytics assistant (web only)

The web dashboard exposes a floating, draggable report assistant with a global provider/model selection. Built-in Kimi and MiniMax appear after authenticated model discovery succeeds; MiniMax remains restricted to the exact `MiniMax-M3` model. The Config page can also add OpenAI Chat Completions-compatible endpoints, discover `/models`, or register an exact manual model ID and context limit when discovery is unavailable. There is no automatic provider failover, and the TUI does not initialize the agent.

The agent loop and every analytics tool run in the Go backend. Assistant prose streams into the chat while it is generated, and privacy-safe cards show each allowlisted analytics tool as it starts and finishes. A lead analyst answers the question and can delegate a focused investigation to a bounded specialist — trend, cost, tooling, workload, or integrity — whose task, finding, tool calls, and token usage are shown nested under the delegation. The browser never receives the MiniMax credential or provider reasoning, and never calls MiniMax directly. Valid tool arguments are strict-normalized before source execution; rejected proposals are redacted. Tools are read-only and aggregate-only: raw transcripts, coding prompts/reasoning, patches, coding-tool payloads, configs, secrets, paths, raw diagnostics/errors, raw event/per-session timestamps, and raw request/session identifiers are outside the allowlist; aggregate UTC day/hour labels are retained for trends. The disclosed current route, source, structured range, and browser timezone can accompany the question as navigation context. Reports name what they rank — model, provider, and tool identifiers are published product names and travel as recorded, and projects travel as their leaf name (`/home/you/work/alpha` → `alpha`) with a stable reference; anything shaped like a path, URL, or other local state is replaced with a process-scoped pseudonym instead. Cross-source costs remain separated by provenance. The assistant uses `requests` for outbound-attempt questions, keeps `messages` for transcript semantics, and must disclose Kimi trace/usage gaps and truncated evidence instead of interpreting unavailable values as zero.

Answers can carry figures. The lead analyst may write a ` ```chart ` block — a bar ranking, a column or stacked composition, a line or area trend, a donut share, or a heatmap — or a ` ```mermaid ` flowchart, sequence diagram, or pie, and the panel renders it as a titled figure with a hover tooltip, a data-table view, and a copy control. No charting or diagramming package is bundled: the specs are parsed, validated, and laid out by local pure modules and drawn as real SVG, so diagram text can never become markup and the dashboard still works offline. Every charted number has to come from a tool result in that turn, values the evidence does not establish are drawn as gaps rather than zeros, and a figure never replaces the prose disclosure of what is missing.

Completed conversations are saved locally with everything that produced them — tool inputs and outputs, specialist findings, per-turn provider token usage, timing, and the view the question was asked from — so a saved conversation restores exactly what was shown live. Assistant history lives in its own SQLite database and is never migrated: a database from a different schema version is rebuilt empty and the reset is logged.

Configure a provider in **Config → Assistant providers**, select a model, and open the floating assistant. Kimi prefers `OPENCODE_DASHBOARD_KIMI_API_KEY` (and optional `OPENCODE_DASHBOARD_KIMI_BASE_URL`) before reusing Kimi Code OAuth; MiniMax uses `OPENCODE_DASHBOARD_MINIMAX_API_KEY` before the existing OpenCode auth-store fallback. Custom keys are optional for unauthenticated local servers and are stored as plaintext in the hardened local settings DB; they are never returned by the API. HTTP requires an explicit warning acknowledgement and is limited to loopback/private-LAN IP endpoints. The first-use disclosure is bound to the provider destination. A model switch at the same endpoint keeps consent; a provider or endpoint change renews it. A complete run is limited to 90 seconds by default; `OPENCODE_DASHBOARD_ASSISTANT_TIMEOUT` accepts `10s` through `5m`, with `OPENCODE_DASHBOARD_MINIMAX_TIMEOUT` retained as a deprecated fallback. See [the agent architecture, tool contracts, loop limits, and privacy model](docs/analytics-assistant.md).

## Prerequisites

| Requirement | Version | Notes |
|-------------|---------|-------|
| At least one source | — | OpenCode DB, Claude Code (`~/.claude`), Codex (`~/.codex`), Kimi Code (`~/.kimi-code`), or Qwen Code (`~/.qwen`) data on disk |
| Go | 1.26+ | Only required to build from source |
| Node.js | 22.12+ | Only required to build from source (frontend); CI pins 22.12.0 |

Released binaries need none of the build tooling — only source data on disk.

## Installation

### Quick install

Install the latest release binary:

```bash
curl -sSL https://raw.githubusercontent.com/Andres77872/opencode-dashboard/master/scripts/install.sh | bash
```

This fetches a **release binary** from GitHub Releases and installs it to `~/.local/bin`.

Verify:

```bash
opencode-dashboard version
```

### Installer environment overrides

These apply to `install.sh` only; see [Runtime environment
variables](#runtime-environment-variables) for the ones the dashboard itself
reads.

| Variable | Default | Purpose |
|----------|---------|---------|
| `VERSION` | `latest` | Pin to a specific release, e.g. `v0.1.12` |
| `NO_CHECKSUM` | `0` | Set to `1` to skip checksum verification |
| `CONFIGURE_PATH` | `0` | Set to `1` to append `~/.local/bin` to your shell rc (`.zshrc`/`.bashrc`/`.profile`) if it is not already on `PATH` |
| `NO_COLOR` | _unset_ | Set to disable colored output |

If `~/.local/bin` is not on your `PATH`, the installer prints the exact
`export` line and which shell rc file to add it to. It never edits your
dotfiles unless you opt in with `CONFIGURE_PATH=1`.

### Version comparison behavior

The installer compares the installed version with the target version:

- **Versions match** — skips install, exits cleanly
- **Versions differ** — installs the target version (including downgrades)

To install a specific version:

```bash
VERSION=v0.1.12 curl -sSL https://raw.githubusercontent.com/Andres77872/opencode-dashboard/master/scripts/install.sh | bash
```

### Updating

Once installed, upgrade in place with the built-in command — it runs the same
official installer for you:

```bash
opencode-dashboard update                 # update to the latest release
opencode-dashboard update --check         # report current vs latest, install nothing
opencode-dashboard update --version v0.1.20  # install a specific version
opencode-dashboard update --force         # reinstall even if already up to date
opencode-dashboard update --no-checksum   # skip release checksum verification
```

`update` prints the current and latest versions, then downloads and atomically
replaces the binary in `~/.local/bin` (safe to run even while another instance
is open). It is bash-only and supported on Linux and macOS.

### Build from source

```bash
git clone https://github.com/Andres77872/opencode-dashboard.git
cd opencode-dashboard
VERSION=v0.1.12 ./scripts/build.sh
cp build/opencode-dashboard ~/.local/bin/
```

## Usage

```
opencode-dashboard <command> [flags]

Commands:
  web        Run the local web dashboard and API server
  tui        Run the local terminal dashboard
  version    Print version and build metadata
  uninstall  Remove dashboard-owned local files
  update     Update to the latest release (or a specific version)
  claude-statusline  Claude Code statusline command that records Pro/Max
             rate limits for the dashboard quota view
```

### Web dashboard

```bash
opencode-dashboard web                          # Default port 7450, OpenCode source
opencode-dashboard web --port 9090              # Custom port
opencode-dashboard web --source codex           # Start on a different source
opencode-dashboard web --db /path/to/db         # Explicit OpenCode DB path
opencode-dashboard web --channel beta           # Channel-specific OpenCode DB
opencode-dashboard web --claude-home ~/.claude  # Explicit Claude Code home
opencode-dashboard web --codex-home ~/.codex    # Explicit Codex home
opencode-dashboard web --kimi-home ~/.kimi-code # Explicit Kimi Code home
opencode-dashboard web --qwen-home ~/.qwen      # Explicit Qwen Code home
opencode-dashboard web --cache-db /tmp/usage.db # Explicit dashboard cache
opencode-dashboard web --rebuild-cache          # Remove dashboard cache before start
opencode-dashboard web --no-cache               # Start without dashboard cache
opencode-dashboard web --no-open                # Don't auto-open the browser
```

### TUI dashboard

```bash
opencode-dashboard tui                       # Interactive terminal UI
opencode-dashboard tui --source claude_code  # Start on Claude Code
opencode-dashboard tui --source kimi_code    # Start on Kimi Code
opencode-dashboard tui --source qwen_code    # Start on Qwen Code
opencode-dashboard tui --channel latest      # Channel-specific OpenCode DB
opencode-dashboard tui --rebuild-cache       # Remove dashboard cache before start
opencode-dashboard tui --no-cache            # Start without dashboard cache
```

Key bindings:

| Keys | Action |
|------|--------|
| `1`–`7` | Jump to tab (Overview, Daily, Models, Tools, Projects, Sessions, Config) |
| `←`/`→`, `h`/`l`, `[`/`]` | Previous / next tab |
| `↑`/`↓`, `k`/`j`, `g`/`G` | Move / jump to top / bottom |
| `p` / `n` | Previous / next page |
| `enter` / `space` | Open detail or drill-down overlay |
| `S` | Switch data source |
| `T` | Open the time-range picker |
| `t` | Cycle the displayed metric |
| `d` | Daily: toggle the overall / requested-processing-mode lens (Codex only) |
| `/` | Filter the current table |
| `s` | Sort the current table |
| `r` | Refresh |
| `?` | Help · `esc` close overlay · `q` quit |

### Flags

| Flag | Commands | Description |
|------|----------|-------------|
| `--port <n>` | `web` | Localhost port to bind (default `7450`) |
| `--db <path>` | `web`, `tui` | Explicit OpenCode SQLite database path |
| `--channel <c>` | `web`, `tui` | Resolve a channel-specific OpenCode DB (`stable`/`latest`/`beta`/custom) |
| `--source <id>` | `web`, `tui` | Initial source: `opencode`, `claude_code`, `codex`, `kimi_code`, or `qwen_code` (default `opencode`) |
| `--claude-home <dir>` | `web`, `tui` | Claude Code config directory |
| `--codex-home <dir>` | `web`, `tui` | Codex config directory |
| `--kimi-home <dir>` | `web`, `tui` | Kimi Code home directory |
| `--qwen-home <dir>` | `web`, `tui` | Qwen Code home directory |
| `--cache-db <path>` | `web`, `tui` | Dashboard-owned SQLite cache path |
| `--rebuild-cache` | `web`, `tui` | Delete the dashboard cache before start |
| `--no-cache` | `web`, `tui` | Run against live sources without using the dashboard cache |
| `--no-open` | `web` | Do not launch the browser automatically |

`--no-cache` and `--rebuild-cache` are mutually exclusive.

### Runtime environment variables

Every path override below is outranked by its equivalent flag.

| Variable | Read by | Purpose |
|----------|---------|---------|
| `OPENCODE_DASHBOARD_DB` | `web`, `tui` | OpenCode SQLite database path (below `--db` and `--channel`) |
| `OPENCODE_DASHBOARD_CACHE_DB` | `web`, `tui` | Dashboard usage-cache path (below `--cache-db`) |
| `CLAUDE_CONFIG_DIR` | `web`, `tui` | Claude Code config directory (below `--claude-home`) |
| `OPENCODE_DASHBOARD_CODEX_HOME` | `web`, `tui` | Codex home (below `--codex-home`) |
| `KIMI_CODE_HOME` | `web`, `tui` | Kimi Code home (below `--kimi-home`); also used by Kimi Code itself |
| `QWEN_CODE_HOME` | `web`, `tui` | Qwen Code home (below `--qwen-home`) |
| `OPENCODE_DASHBOARD_MINIMAX_API_KEY` | `web` | Enables the [analytics assistant](#analytics-assistant-web-only); falls back to opencode's auth store |
| `OPENCODE_DASHBOARD_MINIMAX_BASE_URL` | `web` | MiniMax API base override (China region, integration tests); never accepted from the browser |
| `OPENCODE_DASHBOARD_KIMI_API_KEY` | `web` | Kimi assistant API key; falls back to the existing Kimi Code OAuth login |
| `OPENCODE_DASHBOARD_KIMI_BASE_URL` | `web` | Kimi assistant API base override |
| `OPENCODE_DASHBOARD_ASSISTANT_TIMEOUT` | `web` | Provider-neutral whole-run assistant budget, `10s`–`5m` (default `90s`) |
| `OPENCODE_DASHBOARD_MINIMAX_TIMEOUT` | `web` | Deprecated timeout fallback used only when the generic variable is unset |

Setting a source's home directory — by flag or by environment variable — also
registers that source even when its data is missing, so it appears as
*unavailable* with a diagnostic instead of silently disappearing.

### Time ranges

Every view honors a global time range. Presets:

- **Rolling hours** — `1h`, `6h`, `12h`, `24h`, `72h`
- **Calendar days** — `1d`, `7d` (default), `14d`, `30d`, `1y`
- **All** — `all`, from the earliest recorded activity
- **Custom** — an explicit `from`/`to` date range

Pick a range with `T` in the TUI or the period picker in the web UI; the web UI persists your source and range selections across views and reloads.

### Other commands

| Command | Description |
|---------|-------------|
| `opencode-dashboard version` | Print build info |
| `opencode-dashboard version --short` | Print just the version string |
| `opencode-dashboard update` | Update to the latest release |
| `opencode-dashboard update --check` | Report current vs latest, install nothing |
| `opencode-dashboard uninstall --dry-run` | Preview removal without deleting |
| `opencode-dashboard uninstall --force` | Skip the confirmation prompt |
| `opencode-dashboard claude-statusline --install` | Configure Claude Code's statusline to record Pro/Max rate limits for the [quota view](#quota-tracking) |

## Uninstall

opencode-dashboard has a built-in uninstall command that removes **dashboard-owned** files only:

```bash
opencode-dashboard uninstall --dry-run    # Preview what would be removed
opencode-dashboard uninstall --force      # Remove without confirmation
```

**Removed:**

| Target | Path | Condition |
|--------|------|-----------|
| Binary | `~/.local/bin/opencode-dashboard` | If not currently running |
| Data dir | `~/.local/share/opencode-dashboard` | If exists — the usage cache, pricing aliases, saved assistant conversations, and the Claude quota snapshot |
| Config dir | `~/.config/opencode-dashboard` | If exists |
| State dir | `~/.local/state/opencode-dashboard` | If exists |

Removing the data directory discards user-authored state that is **not**
rebuildable — pricing aliases and saved assistant conversations. Use
`--dry-run` first if you want to keep them.

**Never removed:**

| Path | Reason |
|------|--------|
| `~/.local/share/opencode/`, `~/.config/opencode/` | OpenCode-owned data and config |
| `opencode*.db` | Channel databases |
| `~/.claude`, `~/.codex`, `~/.kimi-code`, `~/.qwen` | Claude Code / Codex / Kimi Code / Qwen Code source data |

`uninstall` never edits `~/.claude/settings.json`, so a statusline entry
installed by `claude-statusline --install` stays behind. Remove that
`statusLine` block yourself if you uninstall the binary.

## API endpoints

The web command also serves a JSON API under `/api/v1`. Most endpoints accept a `?source=<id>` parameter (`opencode`, `claude_code`, `codex`, `kimi_code`, or `qwen_code`; omitted values use the API compatibility default, `opencode`) and a time-range parameter — either `?period=<preset>` or an explicit `?from=YYYY-MM-DD&to=YYYY-MM-DD` (defaults to `7d`). The web client sends its startup-selected source explicitly.

| Endpoint | Description | Notable params |
|----------|-------------|----------------|
| `GET /api/v1/sources` | Registered sources, availability, and capabilities | — |
| `GET /api/v1/overview` | Aggregate sessions, requests, transcript messages, tokens, cost, and optional Kimi request coverage for one source | `source`, period |
| `GET /api/v1/overview/all` | Cross-source merged additive totals (including requests); a lean all-model usage payload when `dimension=model` | period, `trend=true`, `top=<n>`, `dimension=source\|model` |
| `GET /api/v1/daily` | Time-series request/message/token/cost breakdown | `granularity=hour\|day`, `dimension=model\|tool\|project\|processing_mode` (last is Codex), period |
| `GET /api/v1/models` | Model usage statistics | `source`, period |
| `GET /api/v1/tools` | Tool invocation statistics | `source`, period |
| `GET /api/v1/projects` | Per-project aggregation | `source`, period |
| `GET /api/v1/projects/{id}` | Project detail | `page`, `limit`, period |
| `GET /api/v1/sessions` | Paginated session list | `page`, `limit` (≤100), `filter`, `project_id`, period |
| `GET /api/v1/sessions/{id}` | Session detail | `source` |
| `GET /api/v1/messages` | Paginated message list | `page`, `limit` (≤100), `sort`, period |
| `GET /api/v1/messages/{id}` | Message detail | `source` |
| `GET /api/v1/config` | Source configuration preview (redacted) | `source` |
| `GET /api/v1/cache` | Dashboard cache status: per-source readiness, freshness, and any running sync job | — |
| `POST /api/v1/cache/sync` | Start an incremental resync or a clear-and-rebuild | `source`, `mode=incremental\|rebuild` (default `incremental`); `rebuild` ignores `source` and always rebuilds every source |
| `GET /api/v1/pricing/aliases` | [Pricing aliases](#pricing-aliases) for one source, plus every source's catalog and the models this source has observed | `source` |
| `POST /api/v1/pricing/aliases` | Create or replace one alias | JSON body: `source_id`, `provider_id`, `model_id`, `target_model_id`, optional `target_source_id` |
| `DELETE /api/v1/pricing/aliases` | Remove one alias | `source`, `provider`, `model` |
| `GET /api/v1/quotas` | Provider quota (Codex / Claude Code / Kimi Code / MiniMax), including Kimi Extra Usage when available | — |
| `GET /api/v1/assistant/status` | Active provider/model/context, revision, destination consent metadata, and specialist roster | — |
| `GET`/`POST /api/v1/assistant/providers` | List providers or create a custom OpenAI-compatible provider | provider definition on create |
| `PATCH`/`DELETE /api/v1/assistant/providers/{id}` | Update or remove a custom provider | safe provider fields only |
| `POST /api/v1/assistant/providers/{id}/models/refresh` | Refresh authenticated model discovery without erasing a last-good catalog | — |
| `PUT /api/v1/assistant/providers/{id}/models` | Add/update an exact manual model and context limit | model ID and context limit |
| `PUT /api/v1/assistant/selection` | Change the global provider/model used by the next turn | provider and model IDs |
| `POST /api/v1/assistant/chat` | Run the backend report-agent loop | bounded user/assistant history |
| `POST /api/v1/assistant/chat/stream` | Stream assistant text plus privacy-safe round, tool, and specialist lifecycle events (NDJSON) | bounded user/assistant history |
| `GET /api/v1/assistant/sessions` | List saved conversations with their totals | — |
| `GET /api/v1/assistant/sessions/{id}` | Restore one conversation with its tool calls, specialist runs, and usage | — |
| `DELETE /api/v1/assistant/sessions/{id}` | Delete a saved conversation | — |
| `GET /api/v1/version` | Build info | — |
| `GET /health` | Health check | — |

The base `/overview/all?trend=true` response powers the source-grouped Usage
charts and deliberately leaves `top_models` empty, avoiding a model-ranking
scan during cold load. `/overview/all?dimension=model&trend=true` is
intentionally lean and returns only complete, source-tagged `model_usage`
totals plus `model_trend` rows and per-source errors. The web client requests
that payload lazily when Model is selected, reuses it for the Top Models card,
and does not repeat the overview, project, or tool scans.

## Analytics surfaces

Both web and TUI expose the same seven surfaces:

| Surface | Description |
|---------|-------------|
| Overview | Combined sessions, requests, transcript messages, and tokens plus cross-source Usage charts switchable between source and model |
| Daily | Request/message time series, auto hour/day granularity, with per-dimension breakdowns and transcript history |
| Models | Usage by model and provider |
| Tools | Tool invocation counts and patterns |
| Projects | Per-project aggregation, with project detail drill-down |
| Sessions | Paginated browser with session detail |
| Config | Redacted configuration preview for the selected source |

Sessions and daily entries drill down into individual messages.

Two capabilities are web-only by design: the [pricing-alias editor](#pricing-aliases), which lives on the Config surface, and the optional floating [analytics assistant](#analytics-assistant-web-only). The TUI stays fully local and never constructs an outbound LLM client.

## Building from source

### Production build

```bash
VERSION=v0.1.12 ./scripts/build.sh
```

Build flow:

1. `npm ci` — install frontend dependencies
2. `npm run build` — `tsc` type-check + Vite build into `web/dist/`
3. Copy `web/dist/` to `internal/web/dist/` for embedding
4. `go build -tags embedassets` — single binary with the embedded SPA

The `embedassets` build tag is required for production builds. Without it the binary serves a placeholder page (the API still works).

### Development build

```bash
./scripts/dev.sh                 # Build frontend + embed + run on port 7450
./scripts/dev.sh --port 9090     # Custom port
./scripts/dev.sh --no-cache      # Skip cache during local development
```

For a fast frontend-only loop, run the Vite dev server, which serves on `:7451` and proxies `/api` and `/health` to a running `web` instance:

```bash
opencode-dashboard web --no-open   # API on :7450 in one terminal
cd web && npm run dev              # Vite dev server on :7451 in another
```

## Frontend

The web UI is a Vite + React 19 + TypeScript SPA built on **Vael**, an in-house component system: inline-style components under `web/src/components/vael`, CSS design tokens (`web/src/styles/tokens`), self-hosted fonts (Hanken Grotesk, JetBrains Mono), SVG icons drawn from a local path table, and pure-SVG charts — no Radix, Recharts, or icon libraries. Tailwind CSS v4 is loaded for layout utilities alongside the tokens; the only other runtime UI dependencies are `react-router-dom` and `react-day-picker`/`date-fns`, used by the custom-range period picker. The compiled assets are embedded into the Go binary at build time.

See [`web/README.md`](web/README.md) for the frontend's layout, scripts, tests, and conventions.

## Project structure

```
opencode-dashboard/
├── cmd/opencode-dashboard/          # CLI entry point, source wiring, cache runtime
│   ├── main.go
│   └── claude_statusline.go         # claude-statusline subcommand
├── internal/
│   ├── config/                      # XDG paths, DB/channel + source-home resolution
│   ├── cache/                       # Dashboard-owned SQLite aggregate cache
│   ├── chatstore/                   # Saved analytics-assistant conversations
│   ├── pricingalias/                # Durable user-authored pricing aliases
│   ├── analyticsagent/              # Provider-neutral report agent, transports, tools, specialists
│   ├── quota/                       # Codex / Claude / Kimi / MiniMax quota collectors
│   ├── store/                       # SQLite read-only store (OpenCode)
│   ├── source/                      # Source registry + cross-source aggregate
│   │   ├── opencode/                # OpenCode (SQLite) source
│   │   ├── claudecode/              # Claude Code (JSONL) source
│   │   ├── codex/                   # Codex (JSONL) source
│   │   ├── kimicode/                # Kimi Code (state + wire JSONL) source
│   │   └── qwencode/                # Qwen Code (transcripts + token-usage JSONL) source
│   ├── stats/                       # Period, aggregation, and view domain types
│   ├── web/                         # HTTP server, API handlers, embedded SPA
│   ├── tui/                         # Bubble Tea terminal UI
│   ├── update/                      # Self-update via the official installer
│   ├── uninstall/                   # Self-cleanup
│   └── version/                     # Build metadata
├── web/                             # Vite + React + Vael frontend
├── docs/
│   └── analytics-assistant.md       # Assistant architecture and privacy model
├── scripts/
│   ├── build.sh                     # Production build (frontend + embed + binary)
│   ├── dev.sh                       # Dev build + run
│   └── install.sh                   # Curl-pipe installer
├── .goreleaser.yaml                 # Release configuration
├── go.mod                           # Go 1.26, pure-Go SQLite (CGO-free)
└── LICENSE                          # Apache 2.0
```

`web/` carries its own `go.mod` on purpose. It contains no Go code; the file
exists only to cut the subtree out of the root module, because an npm
dependency vendors a Go package that would otherwise be compiled by
`go build ./...` once `node_modules` exists.

## Development

```bash
go test ./...                        # Run all Go tests
go test -short -race ./...           # What CI runs
cd web && npm run lint               # Frontend lint
cd web && npm run build              # Type-check + frontend build
cd web && npm run test:source-state  # Frontend unit tests (node --test)
```

The frontend tests cover the pure logic in `web/src/lib` — source selection,
persisted preferences, formatting, request accounting, pricing aliases,
markdown/syntax rendering, chart-spec and diagram parsing with its layout
arithmetic, and assistant stream parsing. They run on Node's built-in test
runner, so no test framework is installed.

## Limitations

- **Read-only** — cannot modify any source database, transcript, or settings.
- **Cache refresh** — the window after the finality cutoff is read live from raw content on every read and merged, so recent activity is always current (one live pass is memoized for ~2s so the several aggregates behind a single page load share it). Hours older than the cutoff are treated as final and served from the cache; consolidation of newly finalized hours happens in the background every 30 minutes, or on a read once the last successful sync is over seven hours old, retried at most once a minute. Reads never block on it. To re-read full history — after correcting pricing, for example — use the rebuild action.
- **Release targets** — Linux and macOS on `amd64` and `arm64`, built CGO-free with embedded assets (see `.goreleaser.yaml`).

## License

Apache 2.0 — Copyright 2026 arz.ai
