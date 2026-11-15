# opencode-dashboard frontend

The web dashboard SPA: Vite + React 19 + TypeScript, built on **Vael**, the
in-house component system. The compiled bundle is copied to
`internal/web/dist/` and embedded into the Go binary, so this directory
produces no artifact you run on its own.

See the [root README](../README.md) for what the dashboard does and
[`docs/analytics-assistant.md`](../docs/analytics-assistant.md) for the
assistant's architecture and privacy model.

## Development

The SPA has no data of its own — every view reads the Go API. Run the backend
first, then the Vite dev server against it:

```bash
opencode-dashboard web --no-open   # API on 127.0.0.1:7450, one terminal
npm install                        # first time only
npm run dev                        # Vite on localhost:7451, another terminal
```

Vite proxies `/api` and `/health` to `127.0.0.1:7450` and binds port 7451 with
`strictPort`, so a port clash fails loudly instead of silently moving the dev
server somewhere the backend's origin check will reject. That exact dev origin
is the only non-dashboard origin the assistant endpoints accept.

To develop against a backend on another port — a scratch instance with its own
`XDG_DATA_HOME`, say, so the real dashboard's cache and settings are left alone
— point the proxy at it:

```bash
OCD_API_TARGET=http://127.0.0.1:8399 npm run dev
```

Reads work against any target. Changes (toggling a source, saving a pricing
alias, starting a sync) do not: the backend accepts the dev server's origin
only when it listens on the default port, and answers `403` otherwise. That
guard is what keeps another local page from driving the dashboard, so it is
not relaxed for development — exercise changes on the default port, or against
the embedded build.

For a full build that exercises the embed path instead, use `../scripts/dev.sh`
from the repository root.

## Scripts

| Command | Purpose |
|---------|---------|
| `npm run dev` | Vite dev server with HMR, proxying the API to `:7450` (or `OCD_API_TARGET`) |
| `npm run build` | `tsc -b` project type-check, then the Vite production build into `dist/` |
| `npm run lint` | ESLint over the whole package |
| `npm test` | Both test suites below |
| `npm run test:source-state` | Unit tests for `src/lib` on Node's built-in runner |
| `npm run test:components` | Component tests (`*.component.test.tsx`) on Vitest + Testing Library |
| `npm run preview` | Serve a built `dist/` locally |

`npm run build` type-checks before bundling, so a type error fails the build —
that is the type-check step in CI, not a separate command.

## Tests

There are two suites, both next to the code they cover:

- **Pure logic** — `src/lib/<name>.test.ts`, run on `node --test` with no test
  framework. They cover source selection and persisted preferences, period
  labels, formatting and token breakdowns, request accounting and processing
  modes, the resource cache, pricing-alias state, markdown and syntax
  rendering, and assistant stream parsing, history, and transcript handling.
  New files are not picked up automatically: `test:source-state` lists its
  files explicitly in `package.json`, so add yours there.
- **Components** — `*.component.test.tsx`, run on Vitest with Testing Library
  in jsdom. Use these for behavior that only exists once something renders:
  a form's validation and submit flow, what a refresh re-fetches.

Prefer the first kind. Logic that can be a pure function in `src/lib` should be
one, and tested there.

## Data loading

Views do not fetch in ad-hoc effects. `lib/use-cached-resource.ts` is the one
place that fetches, caches and refreshes:

- Results live in an app-wide cache (`lib/resource-cache.ts`), so returning to
  a view shows its data at once.
- It is stale-while-revalidate. A refresh — the button, a finished database
  sync, or coming back to the tab after a minute — refetches in the background
  and never blanks what is on screen. `loading` can be true while `data` is
  set: render the skeleton only when there is no data yet.
- A change of scope (source, period) aborts the in-flight request.

`usePeriodResource(fetcher, period, { cacheId })` wraps it for the common
"one endpoint per source and period" case; give each endpoint its own
`cacheId`. `DashboardContext` carries the refresh generation (`refreshNonce`)
and `requestRefresh()`.

## Layout

```
src/
├── App.tsx              # Routes; every view is lazy-loaded
├── views/               # One module per dashboard surface
├── components/
│   ├── vael/            # The component system (see below)
│   ├── layout/          # Shell: sidebar, topbar, period picker, quota strip
│   ├── config/          # Config surface, including the pricing-alias editor
│   ├── quotas/          # Provider quota cards
│   ├── source/          # Source picker and availability notices
│   └── assistant/       # Floating analytics assistant (web-only)
├── lib/                 # API client, hooks, and pure logic (all tested here)
├── styles/tokens/       # CSS custom properties: colors, typography, spacing, effects
└── types/api.ts         # Response types mirroring the Go API
```

## Vael

Vael is local to this repository, not a published package. Components live in
`src/components/vael` and are styled with inline styles that read CSS custom
properties from `src/styles/tokens`, so theming happens in one place and no
CSS-in-JS runtime is shipped.

There is no Radix, no Recharts, and no icon library: charts are hand-written
SVG (`charts.tsx`, `chart-utils.ts`), and icons are SVG paths in
`icon-paths.ts`. Fonts are self-hosted through `@fontsource` (Hanken Grotesk,
JetBrains Mono) rather than fetched from a CDN, which keeps the dashboard
working fully offline.

The assistant's artifacts follow the same rule. A ` ```chart ` fence is parsed
by `lib/chart-spec.ts` and a ` ```mermaid ` fence by `lib/mermaid.ts` — both
pure, both unit-tested, including the diagram's layout arithmetic — and the
components in `components/assistant` render the result as real SVG. No charting
or diagramming package is installed: `mermaid` would need
`dangerouslySetInnerHTML` for text a model wrote, which the panel does not do.
See [`docs/analytics-assistant.md`](../docs/analytics-assistant.md#visual-artifacts)
for the spec the assistant is prompted to emit.

Tailwind CSS v4 is imported in `index.css` for layout utilities alongside the
tokens. The remaining runtime UI dependencies are `react-router-dom` and
`react-day-picker`/`date-fns`, used by the custom-range period picker.

## Conventions

- **Vael first.** Reach for an existing component in `src/components/vael`
  before adding a bespoke one, and add a token before hard-coding a color or
  spacing value.
- **Logic goes in `src/lib`.** Anything worth a test should be a pure module
  there rather than logic embedded in a component — that is what makes the
  `node --test` suite possible without a DOM.
- **Types mirror the API.** `src/types/api.ts` tracks the Go response shapes;
  update it in the same change as the handler.
- **Views are lazy.** Routes in `App.tsx` are code-split; keep new surfaces
  lazy so cold load stays cheap.
