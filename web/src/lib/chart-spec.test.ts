import assert from 'node:assert/strict'
import test from 'node:test'
import {
  chartProvenance,
  formatChartTick,
  formatChartValue,
  isChartFence,
  parseChartSpec,
  sanitizeChartText,
  stackTotals,
  type ChartSpec,
} from './chart-spec.ts'

function ok(source: string, info: string | null = null): ChartSpec {
  const result = parseChartSpec(source, info)
  assert.equal(result.ok, true, `expected a valid spec, got ${JSON.stringify(result)}`)
  return (result as { ok: true; spec: ChartSpec }).spec
}

function fails(source: string, info: string | null = null): { error: string; hint: string | null } {
  const result = parseChartSpec(source, info)
  assert.equal(result.ok, false, `expected a failure, got ${JSON.stringify(result)}`)
  const failure = result as { ok: false; error: string; hint: string | null }
  return { error: failure.error, hint: failure.hint }
}

test('isChartFence accepts chart and plot with an optional type argument', () => {
  assert.equal(isChartFence('chart'), true)
  assert.equal(isChartFence('plot'), true)
  assert.equal(isChartFence('chart bar'), true)
  assert.equal(isChartFence('plot|line'), true)
  assert.equal(isChartFence('CHART:donut'), true)
  assert.equal(isChartFence('json'), false)
  assert.equal(isChartFence('mermaid'), false)
  assert.equal(isChartFence(null), false)
})

test('parses the row shape into one unnamed series', () => {
  const spec = ok(`{
    "type": "bar",
    "title": "Tokens by model",
    "unit": "tokens",
    "source": "kimi-code",
    "period": "7d",
    "data": [
      {"label": "kimi-k2-turbo", "value": 1200000},
      {"label": "kimi-k2", "value": 340000}
    ]
  }`)
  assert.equal(spec.kind, 'bar')
  assert.equal(spec.stacked, false)
  assert.equal(spec.unit, 'tokens')
  assert.deepEqual(spec.labels, ['kimi-k2-turbo', 'kimi-k2'])
  assert.equal(spec.series.length, 1)
  assert.deepEqual(spec.series[0].values, [1200000, 340000])
  assert.equal(chartProvenance(spec), 'kimi-code · 7d')
})

test('parses the series shape with shared labels', () => {
  const spec = ok(`{
    "type": "line",
    "labels": ["2026-07-01", "2026-07-02", "2026-07-03"],
    "series": [
      {"name": "requests", "values": [12, 9, 15]},
      {"name": "sessions", "values": [3, 2, 4]}
    ]
  }`)
  assert.equal(spec.kind, 'line')
  assert.equal(spec.series.length, 2)
  assert.deepEqual(spec.series[1].values, [3, 2, 4])
})

test('takes the chart type from the fence info string when the object omits it', () => {
  const spec = ok('{"data": [{"label": "a", "value": 1}]}', 'chart column')
  assert.equal(spec.kind, 'column')
  const stacked = ok('{"labels":["a"],"series":[{"name":"x","values":[1]},{"name":"y","values":[2]}]}', 'plot|stacked-column')
  assert.equal(stacked.kind, 'column')
  assert.equal(stacked.stacked, true)
})

test('an explicit type in the object wins over the fence hint', () => {
  const spec = ok('{"type":"donut","data":[{"label":"a","value":1}]}', 'chart bar')
  assert.equal(spec.kind, 'donut')
})

test('type aliases normalize onto the supported forms', () => {
  assert.equal(ok('{"type":"pie","data":[{"label":"a","value":1}]}').kind, 'donut')
  assert.equal(ok('{"type":"hbar","data":[{"label":"a","value":1}]}').kind, 'bar')
  assert.equal(ok('{"type":"trend","data":[{"label":"a","value":1}]}').kind, 'line')
  const stacked = ok('{"type":"stacked_bar","data":[{"label":"a","value":1}]}')
  assert.equal(stacked.kind, 'bar')
  assert.equal(stacked.stacked, true)
})

test('null values stay unknown and are never coerced to zero', () => {
  const spec = ok(`{
    "type": "column",
    "unit": "usd",
    "data": [
      {"label": "day 1", "value": 1.5},
      {"label": "day 2", "value": null},
      {"label": "day 3", "value": "unknown"}
    ]
  }`)
  assert.deepEqual(spec.series[0].values, [1.5, null, null])
  assert.equal(spec.hasUnknown, true)
})

test('numeric strings are accepted only when the reading is unambiguous', () => {
  const spec = ok('{"type":"bar","data":[{"label":"a","value":"1,234"},{"label":"b","value":"$0.42"},{"label":"c","value":"12%"}]}')
  assert.deepEqual(spec.series[0].values, [1234, 0.42, 12])

  const failure = fails('{"type":"bar","data":[{"label":"a","value":"1.2M"}]}')
  assert.match(failure.error, /not a number/)
  assert.match(String(failure.hint), /never write an unknown value as 0/i)
})

test('rows with several measures become one series per measure', () => {
  const spec = ok(`{
    "type": "column",
    "data": [
      {"day": "2026-07-01", "requests": 12, "sessions": 3},
      {"day": "2026-07-02", "requests": 9, "sessions": 2}
    ]
  }`)
  assert.deepEqual(spec.labels, ['2026-07-01', '2026-07-02'])
  assert.deepEqual(spec.series.map((s) => s.name), ['requests', 'sessions'])
  assert.deepEqual(spec.series[0].values, [12, 9])
  assert.deepEqual(spec.series[1].values, [3, 2])
})

test('a measure missing from some rows is unknown for those rows', () => {
  const spec = ok(`{
    "type": "column",
    "data": [
      {"day": "2026-07-01", "requests": 12, "sessions": 3},
      {"day": "2026-07-02", "requests": 9}
    ]
  }`)
  assert.deepEqual(spec.series.map((s) => s.name), ['requests', 'sessions'])
  assert.deepEqual(spec.series[1].values, [3, null])
  assert.equal(spec.hasUnknown, true)
})

test('tuple rows, bare value arrays, and point objects all parse', () => {
  assert.deepEqual(ok('{"type":"bar","data":[["a",1],["b",2]]}').labels, ['a', 'b'])
  assert.deepEqual(ok('{"type":"line","labels":["a","b"],"values":[1,2]}').series[0].values, [1, 2])
  const points = ok('{"type":"line","series":[{"name":"requests","points":[{"label":"mon","value":4},{"label":"tue","value":6}]}]}')
  assert.deepEqual(points.labels, ['mon', 'tue'])
  assert.deepEqual(points.series[0].values, [4, 6])
})

test('a series length mismatch is reported against the label count', () => {
  const failure = fails('{"type":"line","labels":["a","b","c"],"series":[{"name":"x","values":[1,2]}]}')
  assert.match(failure.error, /has 2 values but there are 3 categories/)
  assert.match(String(failure.hint), /one value per label/)
})

test('bounds are enforced with an actionable hint', () => {
  const manySeries = Array.from({ length: 9 }, (_, i) => `{"name":"s${i}","values":[1]}`).join(',')
  assert.match(fails(`{"type":"line","labels":["a"],"series":[${manySeries}]}`).error, /9 series; the limit is 8/)

  const manyPoints = Array.from({ length: 201 }, (_, i) => `{"label":"p${i}","value":1}`).join(',')
  assert.match(fails(`{"type":"line","data":[${manyPoints}]}`).error, /201 points; the limit is 200/)

  const slices = Array.from({ length: 9 }, (_, i) => `{"label":"s${i}","value":1}`).join(',')
  assert.match(fails(`{"type":"donut","data":[${slices}]}`).error, /at most 8 slices/)

  assert.match(
    fails('{"type":"donut","labels":["a"],"series":[{"name":"x","values":[1]},{"name":"y","values":[2]}]}').error,
    /one series of shares/,
  )
})

test('negative values are rejected only where the form cannot show them', () => {
  assert.match(fails('{"type":"donut","data":[{"label":"a","value":-1}]}').error, /negative shares/)
  assert.match(fails('{"type":"heatmap","data":[{"label":"a","value":-1}]}').error, /negative values/)
  assert.match(fails('{"type":"stacked-column","data":[{"label":"a","value":-1}]}').error, /cannot mix negative/)
  assert.match(fails('{"type":"donut","data":[{"label":"a","value":1},{"label":"b","value":null}]}').error, /unknown share/)
  assert.deepEqual(ok('{"type":"column","data":[{"label":"a","value":-1}]}').series[0].values, [-1])
})

test('malformed but recoverable JSON still parses', () => {
  const trailingComma = ok('{"type":"bar","data":[{"label":"a","value":1},],}')
  assert.deepEqual(trailingComma.labels, ['a'])

  const commented = ok(`{
    // tokens for the last week
    "type": "bar",
    "data": [{"label": "a", "value": 1}]
  }`)
  assert.deepEqual(commented.labels, ['a'])

  const wrapped = ok('Here is the chart:\n{"type":"bar","data":[{"label":"a","value":1}]}\n')
  assert.deepEqual(wrapped.labels, ['a'])
})

test('unusable input fails with a usable message instead of throwing', () => {
  assert.match(fails('not json at all').error, /not valid JSON/)
  assert.match(fails('{"data":[{"label":"a","value":1}]}').error, /type is missing or not supported/)
  assert.match(fails('{"type":"bar"}').error, /has no data/)
  assert.match(fails('{"type":"bar","data":[]}').error, /no numeric values|no categories/)
  assert.match(fails('[1,2,3]').error, /single JSON object/)
})

test('text from the model is scrubbed and bounded', () => {
  assert.equal(sanitizeChartText(`a b‮c`, 40), 'a b c')
  assert.equal(sanitizeChartText('  spaced   out  ', 40), 'spaced out')
  assert.equal(sanitizeChartText('x'.repeat(40), 10), `${'x'.repeat(9)}…`)

  const spec = ok('{"type":"bar","title":"A\\u0000B","data":[{"label":"a","value":1}]}')
  assert.equal(spec.title, 'A B')
})

test('spec colors are ignored so the design system owns the palette', () => {
  const spec = ok('{"type":"bar","data":[{"label":"a","value":1,"color":"#ff0000"}],"color":"#00ff00"}')
  assert.equal('color' in spec, false)
  assert.deepEqual(spec.series[0].values, [1])
})

test('values format per unit for tooltips and for axis ticks', () => {
  assert.equal(formatChartValue(null, 'tokens'), 'unknown')
  assert.equal(formatChartValue(1234567, 'tokens'), '1,234,567')
  assert.equal(formatChartValue(0.4231, 'usd'), '$0.4231')
  assert.equal(formatChartValue(12.5, 'percent'), '12.5%')
  assert.equal(formatChartValue(450, 'ms'), '450 ms')
  assert.equal(formatChartValue(90, 'seconds'), '1m 30s')

  assert.equal(formatChartTick(1234567, 'tokens'), '1.2M')
  assert.equal(formatChartTick(25, 'percent'), '25%')
})

test('stack totals skip unknown values without inventing zeros', () => {
  const spec = ok(`{
    "type": "stacked-column",
    "labels": ["a", "b"],
    "series": [
      {"name": "x", "values": [1, null]},
      {"name": "y", "values": [2, null]}
    ]
  }`)
  assert.deepEqual(stackTotals(spec), [3, null])
})
