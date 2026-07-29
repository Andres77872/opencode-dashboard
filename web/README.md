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

For a full build that exercises the embed path instead, use `../scripts/dev.sh`
from the repository root.

## Scripts

| Command | Purpose |
|---------|---------|
| `npm run dev` | Vite dev server with HMR, proxying the API to `:7450` |
| `npm run build` | `tsc -b` project type-check, then the Vite production build into `dist/` |
| `npm run lint` | ESLint over the whole package |
| `npm run test:source-state` | Unit tests for `src/lib` on Node's built-in runner |
| `npm run preview` | Serve a built `dist/` locally |

`npm run build` type-checks before bundling, so a type error fails the build —
that is the type-check step in CI, not a separate command.

## Tests

Tests live next to the module they cover as `src/lib/<name>.test.ts` and run on
`node --test` with no test framework installed. They deliberately cover the
pure logic only — source selection and persisted preferences, period labels,
formatting and token breakdowns, request accounting and processing modes,
pricing-alias state, markdown and syntax rendering, and assistant stream
parsing, history, and transcript handling.

New test files are not picked up automatically: `test:source-state` lists its
files explicitly in `package.json`, so add yours there.

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
