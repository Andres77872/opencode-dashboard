import assert from 'node:assert/strict'
import test from 'node:test'
import {
  highlightSource,
  REDACTION_MARKERS,
  type SyntaxFormat,
  type SyntaxToken,
} from './syntax.ts'

function rejoin(tokens: SyntaxToken[]): string {
  return tokens.map((t) => t.text).join('')
}

function assertLossless(source: string, format: SyntaxFormat) {
  const lines = highlightSource(source, format)
  const originals = source.split('\n')
  assert.equal(lines.length, originals.length)
  for (let i = 0; i < lines.length; i++) {
    assert.equal(rejoin(lines[i]), originals[i], `line ${i + 1} not lossless (${format})`)
  }
}

function types(tokens: SyntaxToken[]): string[] {
  return tokens.map((t) => `${t.type}:${t.text}`)
}

function find(tokens: SyntaxToken[], type: string): SyntaxToken | undefined {
  return tokens.find((t) => t.type === type)
}

/* ---------- lossless invariant ---------- */

const JSON_FIXTURE = `{
  "model": "gpt-5.5",
  "temperature": 0.5,
  "max_tokens": -3,
  "rate": 1.5e-4,
  "stream": true,
  "stop": null,
  "quote": "escaped \\" inside",
  "list": ["a", "b"]
}`

const TOML_FIXTURE = `# top comment
model = "gpt-5.5" # trailing comment
count = 42
pi = 3.14
flag = true
created = 1979-05-27T07:32:00Z
hash_in_string = "#not a comment"

[model_providers.openai]
name = 'literal'
env = { KEY = "value", n = 1 }

[[servers.http]]
host = "a"

multi = """
line two
"""
after = 1`

const YAML_FIXTURE = `---
model: gpt-5.5
temperature: 0.5
stream: true
stop: null
tilde: ~
quoted: "hello"
bare: plain scalar value
commented: value # trailing
list:
  - item
  - key: nested
block: |
  first block line
  second block line
after: done`

test('lossless invariant per format', () => {
  assertLossless(JSON_FIXTURE, 'json')
  assertLossless(TOML_FIXTURE, 'toml')
  assertLossless(YAML_FIXTURE, 'yaml')
})

/* ---------- JSON ---------- */

test('json classifies keys vs string values', () => {
  const [line] = highlightSource('  "model": "gpt-5.5",', 'json')
  const key = find(line, 'key')
  const str = find(line, 'string')
  assert.equal(key?.text, '"model"')
  assert.equal(str?.text, '"gpt-5.5"')
})

test('json handles escaped quotes inside strings', () => {
  const [line] = highlightSource('"a": "x \\" y"', 'json')
  assert.equal(find(line, 'string')?.text, '"x \\" y"')
})

test('json numbers booleans null punctuation', () => {
  const [line] = highlightSource('"n": -1.5e-4, "b": false, "z": null', 'json')
  assert.equal(find(line, 'number')?.text, '-1.5e-4')
  assert.equal(find(line, 'boolean')?.text, 'false')
  assert.equal(find(line, 'null')?.text, 'null')
  assert.ok(find(line, 'punct'))
})

test('json empty line produces no tokens', () => {
  const [line] = highlightSource('', 'json')
  assert.deepEqual(line, [])
})

/* ---------- TOML ---------- */

test('toml comments full-line and trailing', () => {
  const lines = highlightSource('# full\nmodel = "x" # trail', 'toml')
  assert.equal(lines[0][0].type, 'comment')
  assert.equal(find(lines[1], 'comment')?.text, '# trail')
})

test('toml hash inside string is not a comment', () => {
  const [line] = highlightSource('s = "#not"', 'toml')
  assert.equal(find(line, 'comment'), undefined)
  assert.equal(find(line, 'string')?.text, '"#not"')
})

test('toml table headers and array-of-tables are sections', () => {
  const lines = highlightSource('[server]\n[[servers.http]]', 'toml')
  assert.equal(lines[0][0].type, 'section')
  assert.equal(lines[1][0].type, 'section')
  assert.equal(lines[1][0].text, '[[servers.http]]')
})

test('toml dotted keys split on punct', () => {
  const [line] = highlightSource('a.b.c = 1', 'toml')
  const keyTokens = line.filter((t) => t.type === 'key').map((t) => t.text)
  assert.deepEqual(keyTokens, ['a', 'b', 'c'])
  assert.ok(line.some((t) => t.type === 'punct' && t.text === '.'))
})

test('toml literal vs basic strings', () => {
  const [line] = highlightSource(`both = 'literal'`, 'toml')
  assert.equal(find(line, 'string')?.text, "'literal'")
})

test('toml datetime classifies as number', () => {
  const [line] = highlightSource('at = 1979-05-27T07:32:00Z', 'toml')
  assert.equal(find(line, 'number')?.text, '1979-05-27T07:32:00Z')
})

test('toml inline table keys and values', () => {
  const [line] = highlightSource('env = { port = 8080, name = "x" }', 'toml')
  const keys = line.filter((t) => t.type === 'key').map((t) => t.text)
  assert.deepEqual(keys, ['env', 'port', 'name'])
  assert.equal(find(line, 'number')?.text, '8080')
})

test('toml multiline string carries state across lines', () => {
  const lines = highlightSource('m = """\nsecret line\n"""\nafter = 1', 'toml')
  assert.equal(lines[1][0].type, 'string')
  assert.equal(lines[1][0].text, 'secret line')
  assert.equal(lines[2][0].type, 'string')
  assert.equal(find(lines[3], 'key')?.text, 'after')
})

/* ---------- YAML ---------- */

test('yaml key value pairs and list items', () => {
  const lines = highlightSource('model: gpt-5.5\nitems:\n  - item', 'yaml')
  assert.equal(find(lines[0], 'key')?.text, 'model')
  assert.equal(find(lines[0], 'string')?.text, 'gpt-5.5')
  assert.ok(lines[2].some((t) => t.type === 'punct' && t.text === '- '))
})

test('yaml quoted vs bare scalars are both strings', () => {
  const lines = highlightSource('a: "quoted"\nb: bare value', 'yaml')
  assert.equal(find(lines[0], 'string')?.text, '"quoted"')
  assert.equal(find(lines[1], 'string')?.text, 'bare value')
})

test('yaml booleans null tilde numbers', () => {
  const lines = highlightSource('a: true\nb: null\nc: ~\nd: 42', 'yaml')
  assert.equal(find(lines[0], 'boolean')?.text, 'true')
  assert.equal(find(lines[1], 'null')?.text, 'null')
  assert.equal(find(lines[2], 'null')?.text, '~')
  assert.equal(find(lines[3], 'number')?.text, '42')
})

test('yaml trailing comment after value', () => {
  const [line] = highlightSource('a: value # note', 'yaml')
  assert.equal(find(line, 'comment')?.text, '# note')
  assert.equal(find(line, 'string')?.text, 'value')
})

test('yaml document separator is section', () => {
  const [line] = highlightSource('---', 'yaml')
  assert.equal(line[0].type, 'section')
})

test('yaml block scalar consumes indented lines and exits on dedent', () => {
  const lines = highlightSource('block: |\n  inner one\n  inner two\nafter: x', 'yaml')
  assert.equal(lines[1][0].type, 'string')
  assert.equal(lines[2][0].type, 'string')
  assert.equal(find(lines[3], 'key')?.text, 'after')
})

/* ---------- redaction ---------- */

test('redaction markers become redacted tokens in each format', () => {
  for (const [source, format] of [
    ['"apiKey": "[REDACTED]"', 'json'],
    ['api_key = "[REDACTED]"', 'toml'],
    ['api_key: "[REDACTED]"', 'yaml'],
  ] as const) {
    const [line] = highlightSource(source, format)
    const redacted = find(line, 'redacted')
    assert.equal(redacted?.text, '[REDACTED]', `format ${format}: ${JSON.stringify(types(line))}`)
  }
})

test('embedded redacted path splits the surrounding string', () => {
  const [line] = highlightSource('root = "[REDACTED_PATH]/proj-x"', 'toml')
  const redacted = find(line, 'redacted')
  assert.equal(redacted?.text, '[REDACTED_PATH]')
  assert.ok(line.some((t) => t.type === 'string' && t.text.includes('/proj-x')))
})

test('two markers in one line and marker precedence', () => {
  const [line] = highlightSource('x = "[REDACTED_PATH]/a [REDACTED]"', 'toml')
  const redactedTokens = line.filter((t) => t.type === 'redacted').map((t) => t.text)
  assert.deepEqual(redactedTokens, ['[REDACTED_PATH]', '[REDACTED]'])
})

test('REDACTION_MARKERS orders longest first', () => {
  assert.equal(REDACTION_MARKERS[0], '[REDACTED_PATH]')
})

/* ---------- robustness ---------- */

test('garbage input never throws and stays lossless', () => {
  const garbage = 'x = "unterminated\n%%%$$$ ???\n{{{{[[[\n\t\tweird\twhitespace'
  for (const format of ['json', 'toml', 'yaml'] as const) {
    assertLossless(garbage, format)
  }
})
