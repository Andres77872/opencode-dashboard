# Vael — build conventions

Vael is a **dark-first** analytics design system (an observatory for CLI
coding-agent usage). There is no theme provider and no light mode: every screen
sits on the near-black app canvas.

## Setup — the dark canvas

Components are self-contained (no provider/context wrapper needed), but they
are designed for a dark surface. Give every screen a root like:

```jsx
<div style={{ minHeight: '100vh', background: 'var(--ink-900)', color: 'var(--fg-primary)', fontFamily: 'var(--font-ui)' }}>
  …
</div>
```

Without `background: var(--ink-900)` the UI renders dark-on-white and reads as
broken.

## Styling idiom — inline styles + CSS custom properties only

There are **no utility classes** in this system (no Tailwind). Components carry
their own styles; for your layout glue, write inline `style` objects using the
token variables. Never invent colors, fonts, or radii — always `var(--*)`:

- Surfaces (dark → less dark): `--ink-950  --ink-900` (canvas) `--ink-850  --ink-800` (cards) `--ink-750  --ink-700` (controls, menus) `--ink-650  --ink-600`
- Text: `--fg-primary  --fg-secondary  --fg-muted  --fg-faint  --fg-on-accent`
- Borders: `--border-subtle  --border-default  --border-strong  --border-accent`
- Signal blue: `--accent  --accent-hover  --accent-soft  --blue-300`
- Status: `--success  --warning  --danger  --info` (+ `-soft` fills for each)
- Categorical (charts): `--cat-1` … `--cat-8` (+ `-soft`)
- Source/vendor brands: `--vendor-opencode  --vendor-claude  --vendor-codex  --vendor-gemini`
- Type: `--font-ui` (Hanken Grotesk — all UI text) and `--font-mono`
  (JetBrains Mono — every number, id, token count, cost, CLI text; add
  `fontVariantNumeric: 'tabular-nums'` to aligned numerals)
- Shape/effects: `--radius-sm  --radius-md  --radius-lg  --radius-xl  --radius-pill`, `--shadow-sm  --shadow-lg  --shadow-xl`, `--glow-accent`, `--ring-focus`, durations `--dur-fast  --dur-base`, easings `--ease-out  --ease-in-out`

The voice is quiet and precise: sentence case everywhere ("Daily usage", not
"Daily Usage"), no hype, numbers stated plainly.

## Where the truth lives

- `styles.css` → imports `_ds_bundle.css`, which holds every token definition
  (colors, typography scale, spacing, effects) plus the base reset — read it
  before styling anything.
- `components/<group>/<Name>/<Name>.prompt.md` — per-component usage + props;
  `<Name>.d.ts` — the exact API. Valid `Icon`/`iconLeft` names are enumerated
  in `Icon.d.ts`.
- Source ids are `'opencode' | 'claude_code' | 'codex'` (`VendorChip` takes
  them directly; `vendorMeta(id)` and the `VENDORS` map expose names + colors).

## Idiomatic screen fragment

```jsx
const { Card, StatCard, Button, DataTable, VendorChip } = window.Vael;

<div style={{ minHeight: '100vh', background: 'var(--ink-900)', padding: 24, fontFamily: 'var(--font-ui)', color: 'var(--fg-primary)' }}>
  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 12 }}>
    <StatCard label="Total tokens" value="48.2M" delta={{ value: '12.4%', dir: 'up' }} hint="vs previous 30 days" />
    <StatCard label="Est. cost" value="$212.40" delta={{ value: '4.1%', dir: 'down', tone: 'pos' }} />
    <StatCard label="Sessions" value="1,284" unit="runs" accent />
  </div>
  <Card title="Top projects" subtitle="Last 30 days" action={<Button variant="ghost" iconLeft="download">Export</Button>} style={{ marginTop: 16 }}>
    <DataTable
      columns={[
        { key: 'project', header: 'Project', render: (r) => <span style={{ font: '500 13px/1 var(--font-mono)' }}>{r.project}</span> },
        { key: 'source', header: 'Source', render: (r) => <VendorChip id={r.source} /> },
        { key: 'tokens', header: 'Tokens', numeric: true, sortable: true },
      ]}
      rows={[{ project: 'vael-api', source: 'claude_code', tokens: '18.4M' }, { project: 'opencode-dashboard', source: 'opencode', tokens: '9.1M' }]}
    />
  </Card>
</div>
```
