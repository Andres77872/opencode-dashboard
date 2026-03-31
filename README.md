# opencode-dashboard

> Analytics dashboard for OpenCode — single binary, read-only, works without OpenCode running.

View your OpenCode usage: sessions, costs, tokens, models, tools, and projects — through a web dashboard or terminal UI.

## Overview

**opencode-dashboard** reads directly from OpenCode's SQLite database (read-only). Two interfaces:

- **Web dashboard** — Browser-based SPA at `http://127.0.0.1:7450`
- **TUI dashboard** — Terminal interface using Bubble Tea

No OpenCode server required. No configuration needed. Works with any channel (stable, latest, beta).

## Prerequisites

| Requirement | Version | Notes |
|-------------|---------|-------|
| OpenCode | any | The IDE agent with SQLite DB |
| Go | 1.26+ | Build from source only |

## Installation

### Quick Install

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
| `VERSION` | `latest` | Pin to specific release, e.g. `v1.0.0` |
| `NO_CHECKSUM` | `0` | Set to `1` to skip checksum verification |

### Version comparison behavior

The installer compares the installed version with the target version:

- **Versions match** — skips install, exits cleanly
- **Versions differ** — installs the target version (including downgrades)

To install a specific version:

```bash
VERSION=v1.0.0 curl -sSL https://raw.githubusercontent.com/Andres77872/opencode-dashboard/master/scripts/install.sh | bash
```

### Build from source

```bash
git clone https://github.com/Andres77872/opencode-dashboard.git
cd opencode-dashboard
VERSION=v1.0.0 ./scripts/build.sh
cp build/opencode-dashboard ~/.local/bin/
```

## Usage

### Web Dashboard

```bash
opencode-dashboard web                     # Default port 7450
opencode-dashboard web --port 9090         # Custom port
opencode-dashboard web --db /path/to/db    # Explicit DB path
opencode-dashboard web --channel beta      # Channel-specific DB
opencode-dashboard web --no-open           # Skip browser auto-open
```

### TUI Dashboard

```bash
opencode-dashboard tui                     # Interactive terminal UI
opencode-dashboard tui --channel latest    # Channel-specific DB
```

Navigate with arrow keys or `h/l`. Switch tabs with number keys `1-7`. Press `q` to quit.

### Other Commands

| Command | Description |
|---------|-------------|
| `opencode-dashboard version` | Print build info |
| `opencode-dashboard uninstall --dry-run` | Preview removal without deleting |
| `opencode-dashboard uninstall --force` | Skip confirmation prompt |

## Uninstall

opencode-dashboard has a built-in uninstall command that removes project-owned files only:

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
| `~/.local/share/opencode/` | OpenCode-owned data |
| `~/.config/opencode/` | OpenCode-owned config |
| `opencode*.db` | Channel databases |

## Data Source

Reads **read-only** from OpenCode's SQLite database. Never mutates or writes.

Database auto-detection:

| Source | Priority |
|--------|----------|
| `--db` flag | Highest |
| `OPENCODE_DASHBOARD_DB` env | Second |
| `--channel` flag | Third |
| Auto-detect (stable → latest → beta) | Default |

Channel database paths:

| Channel | Path |
|---------|------|
| Stable | `~/.local/share/opencode/opencode.db` |
| Latest | `~/.local/share/opencode/opencode-latest.db` |
| Beta | `~/.local/share/opencode/opencode-beta.db` |

## Building from Source

### Production Build

```bash
VERSION=v1.0.0 ./scripts/build.sh
```

Build flow:

1. `npm ci` — Install frontend dependencies
2. `npm run build` — Vite outputs to `web/dist/`
3. Copy to `internal/web/dist/` for embedding
4. `go build -tags embedassets` — Binary with embedded SPA

The `embedassets` build tag is required for production builds. Without it, the web UI shows a placeholder.

### Development Build

```bash
./scripts/dev.sh                 # Build + run on port 7450
./scripts/dev.sh --port 9090     # Custom port
```

## API Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /api/v1/overview` | Aggregate metrics |
| `GET /api/v1/daily?period=7d\|30d` | Time-series breakdown |
| `GET /api/v1/models` | Model usage statistics |
| `GET /api/v1/tools` | Tool invocation statistics |
| `GET /api/v1/projects` | Per-project aggregation |
| `GET /api/v1/sessions?page=&limit=` | Paginated session list |
| `GET /api/v1/sessions/{id}` | Session detail |
| `GET /api/v1/config` | OpenCode configuration (redacted) |
| `GET /api/v1/version` | Build info |
| `GET /health` | Health check |

## Analytics Surfaces

Both web and TUI provide identical analytics:

| Surface | Description |
|---------|-------------|
| Overview | Total sessions, messages, cost, tokens, cost per day |
| Daily | 7-day or 30-day time series |
| Models | Usage by model and provider |
| Tools | Tool invocation counts and patterns |
| Projects | Per-project aggregation |
| Sessions | Paginated browser with detail view |
| Config | OpenCode configuration preview |

## Project Structure

```
opencode-dashboard/
├── cmd/opencode-dashboard/main.go   # CLI entry point
├── internal/
│   ├── config/                      # XDG paths, DB detection
│   ├── store/                       # SQLite read-only store
│   ├── stats/                       # Aggregation domain
│   ├── web/                         # HTTP server, API handlers
│   │   └── dist/                    # Embedded frontend
│   ├── tui/                         # Bubble Tea terminal UI
│   ├── uninstall/                   # Self-cleanup
│   └── version/                     # Build metadata
├── web/                             # Vite + React + Tailwind frontend
├── scripts/
│   ├── build.sh                     # Production build
│   ├── dev.sh                       # Dev build + run
│   └── install.sh                   # Curl-pipe installer
├── go.mod                           # Go 1.26, pure-Go SQLite
└── LICENSE                          # Apache 2.0
```

## Development

```bash
go test ./...              # Run all tests
cd web && npm run lint     # Frontend lint
cd web && npm run dev      # Dev server (proxies API to port 7450)
```

## Limitations

- **Read-only** — Cannot modify OpenCode database or settings
- **No releases yet** — First release pending from this CI workflow
- **Release targets** — Linux and macOS on amd64 and arm64 (configured in `.goreleaser.yaml`)

## License

Apache 2.0 — Copyright 2026 arz.ai