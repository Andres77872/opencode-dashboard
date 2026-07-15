/* Hand-rolled line tokenizer for config file highlighting (JSON/TOML/YAML).
   DOM/CSS-free so it stays unit-testable with node --test; token colors are
   mapped by the rendering component. Lossless: for every line the token texts
   re-join to the exact input line. Unknown text falls back to 'plain'. */

export type SyntaxFormat = 'json' | 'toml' | 'yaml'

export type SyntaxTokenType =
  | 'key'
  | 'string'
  | 'number'
  | 'boolean'
  | 'null'
  | 'punct'
  | 'comment'
  | 'section'
  | 'redacted'
  | 'plain'

export interface SyntaxToken {
  type: SyntaxTokenType
  text: string
}

/* Longest markers first: '[REDACTED]' is a prefix of neither, but
   '[REDACTED_PATH]' must be matched before '[REDACTED]' would split it. */
export const REDACTION_MARKERS = ['[REDACTED_PATH]', '[REDACTED]'] as const

interface LineState {
  /** TOML multiline string delimiter we are inside of, if any. */
  tomlMultiline: '"""' | "'''" | null
  /** YAML block scalar: indent of the introducing key, or null. */
  yamlBlockIndent: number | null
}

/**
 * Tokenize source into one SyntaxToken[] per line.
 */
export function highlightSource(source: string, format: SyntaxFormat): SyntaxToken[][] {
  const lines = source.split('\n')
  const state: LineState = { tomlMultiline: null, yamlBlockIndent: null }
  return lines.map((line) => {
    let tokens: SyntaxToken[]
    switch (format) {
      case 'json':
        tokens = tokenizeJsonLine(line)
        break
      case 'toml':
        tokens = tokenizeTomlLine(line, state)
        break
      case 'yaml':
        tokens = tokenizeYamlLine(line, state)
        break
    }
    return splitRedacted(tokens)
  })
}

/* ---------- shared scanners ---------- */

const NUMBER_PATTERN = /^[+-]?(?:\d[\d_]*(?:\.[\d_]+)?(?:[eE][+-]?\d+)?|0x[0-9A-Fa-f_]+|0o[0-7_]+|0b[01_]+|inf|nan)/
/* TOML dates/times classify as number-ish literals. */
const DATETIME_PATTERN = /^\d{4}-\d{2}-\d{2}(?:[Tt ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:[Zz]|[+-]\d{2}:\d{2})?)?|^\d{2}:\d{2}:\d{2}(?:\.\d+)?/

function leadingWhitespace(line: string): number {
  return line.length - line.trimStart().length
}

/** Scan a quoted string starting at `start` (line[start] is the quote). */
function scanQuoted(line: string, start: number, allowEscapes: boolean): number {
  const quote = line[start]
  let i = start + 1
  while (i < line.length) {
    if (allowEscapes && line[i] === '\\') {
      i += 2
      continue
    }
    if (line[i] === quote) return i + 1
    i += 1
  }
  return line.length
}

/* ---------- JSON ---------- */

function tokenizeJsonLine(line: string): SyntaxToken[] {
  const tokens: SyntaxToken[] = []
  let i = 0
  while (i < line.length) {
    const c = line[i]
    if (c === '"') {
      const end = scanQuoted(line, i, true)
      const text = line.slice(i, end)
      // A string followed (after whitespace) by ':' is an object key.
      let j = end
      while (j < line.length && (line[j] === ' ' || line[j] === '\t')) j += 1
      tokens.push({ type: line[j] === ':' ? 'key' : 'string', text })
      i = end
      continue
    }
    if (c === ' ' || c === '\t') {
      let j = i
      while (j < line.length && (line[j] === ' ' || line[j] === '\t')) j += 1
      tokens.push({ type: 'plain', text: line.slice(i, j) })
      i = j
      continue
    }
    if ('{}[],:'.includes(c)) {
      tokens.push({ type: 'punct', text: c })
      i += 1
      continue
    }
    const rest = line.slice(i)
    const keyword = /^(true|false|null)\b/.exec(rest)
    if (keyword) {
      tokens.push({ type: keyword[1] === 'null' ? 'null' : 'boolean', text: keyword[1] })
      i += keyword[1].length
      continue
    }
    const num = /^-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/.exec(rest)
    if (num) {
      tokens.push({ type: 'number', text: num[0] })
      i += num[0].length
      continue
    }
    tokens.push({ type: 'plain', text: c })
    i += 1
  }
  return mergeAdjacent(tokens)
}

/* ---------- TOML ---------- */

function tokenizeTomlLine(line: string, state: LineState): SyntaxToken[] {
  if (state.tomlMultiline) {
    const closeAt = line.indexOf(state.tomlMultiline)
    if (closeAt >= 0) {
      const end = closeAt + state.tomlMultiline.length
      state.tomlMultiline = null
      const tokens: SyntaxToken[] = [{ type: 'string', text: line.slice(0, end) }]
      if (end < line.length) tokens.push(...tokenizeTomlValue(line.slice(end), state))
      return tokens
    }
    return line.length > 0 ? [{ type: 'string', text: line }] : []
  }

  const trimmed = line.trim()
  const indent = leadingWhitespace(line)
  const tokens: SyntaxToken[] = []
  if (indent > 0) tokens.push({ type: 'plain', text: line.slice(0, indent) })

  if (trimmed === '') return tokens
  if (trimmed.startsWith('#')) {
    tokens.push({ type: 'comment', text: line.slice(indent) })
    return tokens
  }
  if (trimmed.startsWith('[')) {
    // Table header; a trailing comment may follow the closing bracket.
    const body = line.slice(indent)
    const hash = findCommentStart(body)
    if (hash >= 0) {
      tokens.push({ type: 'section', text: body.slice(0, hash) })
      tokens.push({ type: 'comment', text: body.slice(hash) })
    } else {
      tokens.push({ type: 'section', text: body })
    }
    return tokens
  }

  const eq = findTomlAssignment(line, indent)
  if (eq < 0) {
    tokens.push(...tokenizeTomlValue(line.slice(indent), state))
    return tokens
  }

  const keyPart = line.slice(indent, eq)
  tokens.push(...tokenizeDottedKey(keyPart))
  tokens.push({ type: 'punct', text: '=' })
  tokens.push(...tokenizeTomlValue(line.slice(eq + 1), state))
  return tokens
}

/** Index of the `=` separating key from value, or -1. Keys may be quoted. */
function findTomlAssignment(line: string, from: number): number {
  let i = from
  while (i < line.length) {
    const c = line[i]
    if (c === '"' || c === "'") {
      i = scanQuoted(line, i, c === '"')
      continue
    }
    if (c === '=') return i
    if (c === '#') return -1
    i += 1
  }
  return -1
}

function tokenizeDottedKey(keyPart: string): SyntaxToken[] {
  const tokens: SyntaxToken[] = []
  let i = 0
  while (i < keyPart.length) {
    const c = keyPart[i]
    if (c === '.') {
      tokens.push({ type: 'punct', text: '.' })
      i += 1
      continue
    }
    if (c === ' ' || c === '\t') {
      let j = i
      while (j < keyPart.length && (keyPart[j] === ' ' || keyPart[j] === '\t')) j += 1
      tokens.push({ type: 'plain', text: keyPart.slice(i, j) })
      i = j
      continue
    }
    if (c === '"' || c === "'") {
      const end = scanQuoted(keyPart, i, c === '"')
      tokens.push({ type: 'key', text: keyPart.slice(i, end) })
      i = end
      continue
    }
    let j = i
    while (j < keyPart.length && !'. \t'.includes(keyPart[j])) j += 1
    tokens.push({ type: 'key', text: keyPart.slice(i, j) })
    i = j
  }
  return mergeAdjacent(tokens)
}

function tokenizeTomlValue(value: string, state: LineState): SyntaxToken[] {
  const tokens: SyntaxToken[] = []
  let i = 0
  while (i < value.length) {
    const c = value[i]
    if (c === ' ' || c === '\t') {
      let j = i
      while (j < value.length && (value[j] === ' ' || value[j] === '\t')) j += 1
      tokens.push({ type: 'plain', text: value.slice(i, j) })
      i = j
      continue
    }
    if (c === '#') {
      tokens.push({ type: 'comment', text: value.slice(i) })
      break
    }
    let openedMultiline = false
    for (const delim of ['"""', "'''"] as const) {
      if (value.startsWith(delim, i)) {
        const closeAt = value.indexOf(delim, i + 3)
        if (closeAt >= 0) {
          tokens.push({ type: 'string', text: value.slice(i, closeAt + 3) })
          i = closeAt + 3
        } else {
          tokens.push({ type: 'string', text: value.slice(i) })
          state.tomlMultiline = delim
          i = value.length
        }
        openedMultiline = true
        break
      }
    }
    if (openedMultiline) continue
    if (c === '"' || c === "'") {
      const end = scanQuoted(value, i, c === '"')
      tokens.push({ type: 'string', text: value.slice(i, end) })
      i = end
      continue
    }
    if ('{}[],='.includes(c)) {
      tokens.push({ type: 'punct', text: c })
      i += 1
      continue
    }
    const rest = value.slice(i)
    const keyword = /^(true|false)\b/.exec(rest)
    if (keyword) {
      tokens.push({ type: 'boolean', text: keyword[1] })
      i += keyword[1].length
      continue
    }
    const datetime = DATETIME_PATTERN.exec(rest)
    if (datetime) {
      tokens.push({ type: 'number', text: datetime[0] })
      i += datetime[0].length
      continue
    }
    const num = NUMBER_PATTERN.exec(rest)
    if (num) {
      tokens.push({ type: 'number', text: num[0] })
      i += num[0].length
      continue
    }
    // Bare identifier (e.g. inline-table key); classify as key when an '='
    // follows, otherwise plain.
    const ident = /^[A-Za-z0-9_-]+/.exec(rest)
    if (ident) {
      let j = i + ident[0].length
      while (j < value.length && (value[j] === ' ' || value[j] === '\t')) j += 1
      tokens.push({ type: value[j] === '=' ? 'key' : 'plain', text: ident[0] })
      i += ident[0].length
      continue
    }
    tokens.push({ type: 'plain', text: c })
    i += 1
  }
  return mergeAdjacent(tokens)
}

/** First `#` outside of quotes, or -1. */
function findCommentStart(text: string): number {
  let i = 0
  while (i < text.length) {
    const c = text[i]
    if (c === '"' || c === "'") {
      i = scanQuoted(text, i, c === '"')
      continue
    }
    if (c === '#') return i
    i += 1
  }
  return -1
}

/* ---------- YAML ---------- */

const YAML_KEY_PATTERN = /^("(?:[^"\\]|\\.)*"|'[^']*'|[^\s:#-][^:#]*?)(\s*)(:)(?=\s|$)/

function tokenizeYamlLine(line: string, state: LineState): SyntaxToken[] {
  if (state.yamlBlockIndent !== null) {
    if (line.trim() === '') return line.length > 0 ? [{ type: 'string', text: line }] : []
    if (leadingWhitespace(line) > state.yamlBlockIndent) {
      return [{ type: 'string', text: line }]
    }
    state.yamlBlockIndent = null
  }

  const trimmed = line.trim()
  const indent = leadingWhitespace(line)
  const tokens: SyntaxToken[] = []
  if (indent > 0) tokens.push({ type: 'plain', text: line.slice(0, indent) })

  if (trimmed === '') return tokens
  if (trimmed.startsWith('#')) {
    tokens.push({ type: 'comment', text: line.slice(indent) })
    return tokens
  }
  if (trimmed === '---' || trimmed === '...') {
    tokens.push({ type: 'section', text: line.slice(indent) })
    return tokens
  }

  let rest = line.slice(indent)
  if (rest.startsWith('- ')) {
    tokens.push({ type: 'punct', text: '- ' })
    rest = rest.slice(2)
  } else if (rest === '-') {
    tokens.push({ type: 'punct', text: '-' })
    return tokens
  }

  const keyMatch = YAML_KEY_PATTERN.exec(rest)
  if (keyMatch) {
    tokens.push({ type: 'key', text: keyMatch[1] })
    if (keyMatch[2]) tokens.push({ type: 'plain', text: keyMatch[2] })
    tokens.push({ type: 'punct', text: ':' })
    const valueStart = keyMatch[0].length
    const value = rest.slice(valueStart)
    tokens.push(...tokenizeYamlValue(value, state, indent))
    return tokens
  }

  tokens.push(...tokenizeYamlValue(rest, state, indent))
  return tokens
}

function tokenizeYamlValue(value: string, state: LineState, keyIndent: number): SyntaxToken[] {
  const tokens: SyntaxToken[] = []
  const lead = leadingWhitespace(value)
  if (lead > 0) tokens.push({ type: 'plain', text: value.slice(0, lead) })
  const body = value.slice(lead)
  if (body === '') return tokens

  const hash = findCommentStart(body)
  const scalar = hash >= 0 ? body.slice(0, hash).trimEnd() : body
  const trailing = hash >= 0 ? body.slice(scalar.length) : ''

  if (scalar !== '') {
    if (/^[|>][+-]?\d*$/.test(scalar)) {
      tokens.push({ type: 'punct', text: scalar })
      state.yamlBlockIndent = keyIndent
    } else if (scalar.startsWith('"') || scalar.startsWith("'")) {
      tokens.push({ type: 'string', text: scalar })
    } else if (/^(true|false|yes|no|on|off)$/i.test(scalar)) {
      tokens.push({ type: 'boolean', text: scalar })
    } else if (/^(null|~)$/i.test(scalar)) {
      tokens.push({ type: 'null', text: scalar })
    } else if (NUMBER_PATTERN.test(scalar) && NUMBER_PATTERN.exec(scalar)?.[0] === scalar) {
      tokens.push({ type: 'number', text: scalar })
    } else if (DATETIME_PATTERN.test(scalar) && DATETIME_PATTERN.exec(scalar)?.[0] === scalar) {
      tokens.push({ type: 'number', text: scalar })
    } else {
      // Bare scalars are strings in YAML.
      tokens.push({ type: 'string', text: scalar })
    }
  }
  if (trailing !== '') {
    const ws = leadingWhitespace(trailing)
    if (ws > 0) tokens.push({ type: 'plain', text: trailing.slice(0, ws) })
    tokens.push({ type: 'comment', text: trailing.slice(ws) })
  }
  return tokens
}

/* ---------- redaction + merging ---------- */

/** Split redaction markers out of string/plain tokens into 'redacted' tokens. */
function splitRedacted(tokens: SyntaxToken[]): SyntaxToken[] {
  const result: SyntaxToken[] = []
  for (const token of tokens) {
    if (token.type !== 'string' && token.type !== 'plain' && token.type !== 'key') {
      result.push(token)
      continue
    }
    let remaining = token.text
    while (remaining.length > 0) {
      let markerIndex = -1
      let marker = ''
      for (const candidate of REDACTION_MARKERS) {
        const at = remaining.indexOf(candidate)
        if (at >= 0 && (markerIndex < 0 || at < markerIndex)) {
          markerIndex = at
          marker = candidate
        }
      }
      if (markerIndex < 0) {
        result.push({ type: token.type, text: remaining })
        break
      }
      if (markerIndex > 0) {
        result.push({ type: token.type, text: remaining.slice(0, markerIndex) })
      }
      result.push({ type: 'redacted', text: marker })
      remaining = remaining.slice(markerIndex + marker.length)
    }
  }
  return result.filter((t) => t.text.length > 0)
}

function mergeAdjacent(tokens: SyntaxToken[]): SyntaxToken[] {
  const result: SyntaxToken[] = []
  for (const token of tokens) {
    const last = result[result.length - 1]
    if (last && last.type === token.type) {
      last.text += token.text
      continue
    }
    result.push({ ...token })
  }
  return result.filter((t) => t.text.length > 0)
}
