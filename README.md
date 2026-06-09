# opencode-dashboard

> Local, read-only analytics for your AI coding-assistant usage — one binary, web or terminal, works offline.

See your usage across **OpenCode**, **Claude Code**, and **Codex** — sessions, costs, tokens, models, tools, projects, and messages — through a web dashboard or a terminal UI. Nothing leaves your machine; no servers are started against your data; no files are modified.

## Overview

**opencode-dashboard** reads each tool's local usage data directly and renders it two ways:

- **Web dashboard** — browser SPA served at `http://127.0.0.1:7450`
- **TUI dashboard** — terminal interface built with Bubble Tea

It supports three data sources, all read **read-only** and **local-only**:

| Source | ID | Storage | Default location |
|--------|----|---------|------------------|
| OpenCode | `opencode` | SQLite database | channel DBs under `~/.local/share/opencode/` |
| Claude Code | `claude_code` | JSONL transcripts | `~/.claude` |
| Codex | `codex` | JSONL transcripts | `~/.codex` |

Most views are scoped to one selected source. The **Overview** is the exception: it merges every available source into combined totals plus a per-source breakdown. You can switch the active source and time range live in both interfaces. No OpenCode (or other) server needs to be running, and at least one source's local data is all that's required.

## Data sources

Each source is detected automatically and exposed with its own capabilities, diagnostics, cost policy, and privacy posture. A source that is missing or unreadable is reported as *unavailable* rather than failing the whole dashboard.

| Source | Kind | Resolution order | Cost provenance |
|--------|------|------------------|-----------------|
| OpenCode | `sqlite` | `--db` → `--channel` → `OPENCODE_DASHBOARD_DB` → auto-detect (stable → latest → beta) | `reported` — real spend recorded by OpenCode |
| Claude Code | `jsonl` | `--claude-home` → `CLAUDE_CONFIG_DIR` → `~/.claude` | `mixed` — reported when present, else computed from a bundled pricing snapshot, else missing |
| Codex | `jsonl` | `--codex-home` → `OPENCODE_DASHBOARD_CODEX_HOME` → `~/.codex` | `estimated_api_equivalent` — estimated API-equivalent value, **not** actual subscription spend |

### Cross-source costs

The Overview deliberately does **not** present a single combined cost number. OpenCode reports real dollars, Codex reports an estimated API-equivalent, and Claude Code is mixed — summing them would be misleading. Costs are always shown per source with each source's own provenance, while additive metrics (sessions, messages, tokens, days) are combined. Cross-source "top" signals (models, projects, tools) are ranked by a cost-neutral metric (tokens / invocations) so real and estimated dollars are never compared.

### Privacy

- **Read-only** — no source file or database is ever written to or mutated.
- **Local-only** — data is read from local paths and served on `127.0.0.1`; nothing is uploaded.
- **Plaintext transcripts** — Claude Code and Codex JSONL transcripts are local plaintext and may contain prompts, tool output, file paths, patches, and secrets.
- **Redaction** — config previews (`/api/v1/config`) redact obvious secrets before display.

## Prerequisites

| Requirement | Version | Notes |
|-------------|---------|-------|
| At least one source | — | OpenCode DB, Claude Code (`~/.claude`), or Codex (`~/.codex`) data on disk |
| Go | 1.26+ | Only required to build from source |

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

### Environment overrides

| Variable | Default | Purpose |
|----------|---------|---------|
| `VERSION` | `latest` | Pin to a specific release, e.g. `v0.1.12` |
| `NO_CHECKSUM` | `0` | Set to `1` to skip checksum verification |

### Version comparison behavior

The installer compares the installed version with the target version:

- **Versions match** — skips install, exits cleanly
- **Versions differ** — installs the target version (including downgrades)

To install a specific version:

```bash
VERSION=v0.1.12 curl -sSL https://raw.githubusercontent.com/Andres77872/opencode-dashboard/master/scripts/install.sh | bash
```

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
opencode-dashboard web --no-open                # Don't auto-open the browser
```

### TUI dashboard

```bash
opencode-dashboard tui                       # Interactive terminal UI
opencode-dashboard tui --source claude_code  # Start on Claude Code
opencode-dashboard tui --channel latest      # Channel-specific OpenCode DB
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
| `--source <id>` | `web`, `tui` | Initial source: `opencode`, `claude_code`, or `codex` (default `opencode`) |
| `--claude-home <dir>` | `web`, `tui` | Claude Code config directory |
| `--codex-home <dir>` | `web`, `tui` | Codex config directory |
| `--no-open` | `web` | Do not launch the browser automatically |

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
| `opencode-dashboard uninstall --dry-run` | Preview removal without deleting |
| `opencode-dashboard uninstall --force` | Skip the confirmation prompt |

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
| Data dir | `~/.local/share/opencode-dashboard` | If exists |
| Config dir | `~/.config/opencode-dashboard` | If exists |
| State dir | `~/.local/state/opencode-dashboard` | If exists |

**Never removed:**

| Path | Reason |
|------|--------|
| `~/.local/share/opencode/`, `~/.config/opencode/` | OpenCode-owned data and config |
| `opencode*.db` | Channel databases |
| `~/.claude`, `~/.codex` | Claude Code / Codex source data |

## API endpoints

The web command also serves a JSON API under `/api/v1`. Most endpoints accept a `?source=<id>` parameter (`opencode`, `claude_code`, or `codex`; defaults to the startup source) and a time-range parameter — either `?period=<preset>` or an explicit `?from=YYYY-MM-DD&to=YYYY-MM-DD` (defaults to `7d`).

| Endpoint | Description | Notable params |
|----------|-------------|----------------|
| `GET /api/v1/sources` | Registered sources, availability, and capabilities | — |
| `GET /api/v1/overview` | Aggregate metrics for one source | `source`, period |
| `GET /api/v1/overview/all` | Cross-source merged overview | period, `trend=true`, `top=<n>` |
| `GET /api/v1/daily` | Time-series breakdown | `granularity=hour\|day`, `dimension=<dim>`, period |
| `GET /api/v1/models` | Model usage statistics | `source`, period |
| `GET /api/v1/tools` | Tool invocation statistics | `source`, period |
| `GET /api/v1/projects` | Per-project aggregation | `source`, period |
| `GET /api/v1/projects/{id}` | Project detail | `page`, `limit`, period |
| `GET /api/v1/sessions` | Paginated session list | `page`, `limit` (≤100), `filter`, `project_id`, period |
| `GET /api/v1/sessions/{id}` | Session detail | `source` |
| `GET /api/v1/messages` | Paginated message list | `page`, `limit` (≤100), `sort`, period |
| `GET /api/v1/messages/{id}` | Message detail | `source` |
| `GET /api/v1/config` | Source configuration preview (redacted) | `source` |
| `GET /api/v1/version` | Build info | — |
| `GET /health` | Health check | — |

## Analytics surfaces

Both web and TUI expose the same seven surfaces:

| Surface | Description |
|---------|-------------|
| Overview | Combined totals plus a per-source breakdown (cross-source) |
| Daily | Time series, auto hour/day granularity, with per-dimension breakdowns |
| Models | Usage by model and provider |
| Tools | Tool invocation counts and patterns |
| Projects | Per-project aggregation, with project detail drill-down |
| Sessions | Paginated browser with session detail |
| Config | Redacted configuration preview for the selected source |

Sessions and daily entries drill down into individual messages.

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
```

For a fast frontend-only loop, run the Vite dev server, which proxies the API to a running `web` instance:

```bash
opencode-dashboard web --no-open   # API on :7450 in one terminal
cd web && npm run dev              # Vite dev server in another
```

## Frontend

The web UI is a Vite + React 19 + TypeScript SPA built on **Vael**, an in-house component system: inline-style components under `web/src/components/vael`, CSS design tokens (`web/src/styles/tokens`), self-hosted fonts (Hanken Grotesk, JetBrains Mono), and pure-SVG charts — no Radix, Recharts, or icon libraries. The compiled assets are embedded into the Go binary at build time.

## Project structure

```
opencode-dashboard/
├── cmd/opencode-dashboard/main.go   # CLI entry point and source wiring
├── internal/
│   ├── config/                      # XDG paths, DB/channel + source-home resolution
│   ├── store/                       # SQLite read-only store (OpenCode)
│   ├── source/                      # Source registry + cross-source aggregate
│   │   ├── opencode/                # OpenCode (SQLite) source
│   │   ├── claudecode/              # Claude Code (JSONL) source
│   │   └── codex/                   # Codex (JSONL) source
│   ├── stats/                       # Period, aggregation, and view domain types
│   ├── web/                         # HTTP server, API handlers, embedded SPA
│   ├── tui/                         # Bubble Tea terminal UI
│   ├── uninstall/                   # Self-cleanup
│   └── version/                     # Build metadata
├── web/                             # Vite + React + Vael frontend
├── scripts/
│   ├── build.sh                     # Production build (frontend + embed + binary)
│   ├── dev.sh                       # Dev build + run
│   └── install.sh                   # Curl-pipe installer
├── .goreleaser.yaml                 # Release configuration
├── go.mod                           # Go 1.26, pure-Go SQLite (CGO-free)
└── LICENSE                          # Apache 2.0
```

## Development

```bash
go test ./...              # Run all Go tests
cd web && npm run lint     # Frontend lint
cd web && npm run build    # Type-check + frontend build
```

## Limitations

- **Read-only** — cannot modify any source database, transcript, or settings.
- **Snapshot caching** — JSONL sources (Claude Code, Codex) are re-scanned on a short TTL; very large transcript trees take longer to load.
- **Release targets** — Linux and macOS on `amd64` and `arm64`, built CGO-free with embedded assets (see `.goreleaser.yaml`).

## License

Apache 2.0 — Copyright 2026 arz.ai
