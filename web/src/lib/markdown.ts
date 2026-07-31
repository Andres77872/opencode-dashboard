/**
 * A small, dependency-free Markdown parser tuned for the analytics assistant's
 * streamed responses. It produces a block/inline AST that the Markdown React
 * component renders — parsing stays here so it can be unit-tested in isolation
 * (compare assistant-stream.ts) and so the renderer never touches raw strings.
 *
 * Design constraints:
 *  - Safe by construction. There is no HTML passthrough and links are limited
 *    to a scheme allowlist, so the renderer never needs dangerouslySetInnerHTML.
 *  - Robust on partial input. Streaming deltas routinely end mid-construct; an
 *    unterminated emphasis run or fence degrades to literal/last-block text
 *    instead of throwing.
 *  - Identifier-friendly. Tool names like `get_daily_usage` must survive intact,
 *    so underscore emphasis only fires at word boundaries.
 *
 * Supported: ATX headings, paragraphs (with soft/hard breaks), fenced code
 * blocks, ordered/unordered nested lists, blockquotes, thematic breaks, GFM
 * tables, and inline bold/italic/strikethrough/code/links/autolinks.
 */

export type MdInline =
  | { type: 'text'; value: string }
  | { type: 'strong'; children: MdInline[] }
  | { type: 'em'; children: MdInline[] }
  | { type: 'del'; children: MdInline[] }
  | { type: 'code'; value: string }
  | { type: 'link'; href: string; children: MdInline[] }
  | { type: 'br' }

export type MdTableAlign = 'left' | 'center' | 'right' | null

export type MdBlock =
  | { type: 'heading'; level: number; children: MdInline[] }
  | { type: 'paragraph'; children: MdInline[] }
  /**
   * `closed` is false while a streamed fence is still open. Artifact fences
   * (charts, diagrams) need that distinction: an unfinished spec is a block
   * still arriving, not a malformed one, so the renderer shows progress
   * instead of a parse error that would flash on every delta.
   */
  | { type: 'code'; lang: string | null; value: string; closed: boolean }
  | { type: 'list'; ordered: boolean; start: number; items: MdBlock[][] }
  | { type: 'blockquote'; children: MdBlock[] }
  | { type: 'hr' }
  | { type: 'table'; align: MdTableAlign[]; header: MdInline[][]; rows: MdInline[][][] }

/* ------------------------------------------------------------------ *
 * URL sanitization
 * ------------------------------------------------------------------ */

const ALLOWED_SCHEME = /^(?:https?|mailto|tel):/i
const HAS_SCHEME = /^[a-z][a-z0-9+.-]*:/i

/**
 * Returns a href safe to place in an anchor, or null when the target uses a
 * disallowed scheme (javascript:, data:, vbscript:, …). Schemeless relative and
 * fragment links are allowed; anything carrying an unrecognized scheme is
 * rejected so the caller can fall back to plain text.
 */
export function sanitizeUrl(href: string): string | null {
  const trimmed = href.trim().replace(/^<|>$/g, '')
  if (trimmed === '') return null
  if (trimmed.startsWith('#') || trimmed.startsWith('/')) return trimmed
  if (ALLOWED_SCHEME.test(trimmed)) return trimmed
  // A recognizable but non-allowlisted scheme is unsafe; reject it outright.
  if (HAS_SCHEME.test(trimmed)) return null
  // Otherwise a schemeless relative reference (e.g. "docs/x", "./x").
  return trimmed
}

/* ------------------------------------------------------------------ *
 * Inline parsing
 * ------------------------------------------------------------------ */

interface InlineMatch {
  start: number
  end: number
  make: () => MdInline
}

function pushText(nodes: MdInline[], value: string): void {
  if (value === '') return
  const last = nodes[nodes.length - 1]
  if (last && last.type === 'text') last.value += value
  else nodes.push({ type: 'text', value })
}

function pushNode(nodes: MdInline[], node: MdInline): void {
  if (node.type === 'text') pushText(nodes, node.value)
  else nodes.push(node)
}

// A `code` span keeps at most one padding space on each side (GFM), so authors
// can write `` `x` `` around a token that itself starts with a backtick.
function normalizeCodeSpan(value: string): string {
  if (value.length > 1 && value.startsWith(' ') && value.endsWith(' ') && value.trim() !== '') {
    return value.slice(1, -1)
  }
  return value
}

function makeLink(rawHref: string, label: string): MdInline {
  const href = sanitizeUrl(rawHref)
  if (href === null) return { type: 'text', value: label }
  const children = parseInline(label)
  return { type: 'link', href, children: children.length ? children : [{ type: 'text', value: href }] }
}

/**
 * Finds the earliest inline construct in `text`. Rules are tried in priority
 * order; on a positional tie the earlier rule wins (a strict `<` comparison),
 * which is what keeps `**x**` from being read as nested `*` emphasis.
 */
function firstInline(text: string): InlineMatch | null {
  let best: InlineMatch | null = null
  const add = (re: RegExp, build: (m: RegExpExecArray) => MdInline, endOf?: (m: RegExpExecArray) => number) => {
    const m = re.exec(text)
    if (!m) return
    if (best === null || m.index < best.start) {
      best = { start: m.index, end: endOf ? endOf(m) : m.index + m[0].length, make: () => build(m) }
    }
  }

  // Code spans are literal, so they take precedence over every other marker.
  add(/(`+)([\s\S]*?)\1/, (m) => ({ type: 'code', value: normalizeCodeSpan(m[2]) }))
  // [label](href "title") and its image form (rendered as its alt text).
  add(/(!?)\[([^\]]*)\]\(\s*(<[^>]*>|[^)\s]*)(?:\s+"[^"]*")?\s*\)/, (m) => (
    m[1] === '!' ? { type: 'text', value: m[2] } : makeLink(m[3], m[2])
  ))
  add(/\*\*(\S(?:[\s\S]*?\S)?)\*\*/, (m) => ({ type: 'strong', children: parseInline(m[1]) }))
  add(/(?<![\p{L}\p{N}_])__(\S(?:[\s\S]*?\S)?)__(?![\p{L}\p{N}_])/u, (m) => ({ type: 'strong', children: parseInline(m[1]) }))
  add(/~~(\S(?:[\s\S]*?\S)?)~~/, (m) => ({ type: 'del', children: parseInline(m[1]) }))
  add(/\*(\S(?:[\s\S]*?\S)?)\*/, (m) => ({ type: 'em', children: parseInline(m[1]) }))
  // Underscore emphasis only at word boundaries so identifiers stay intact.
  add(/(?<![\p{L}\p{N}_])_(\S(?:[\s\S]*?\S)?)_(?![\p{L}\p{N}_])/u, (m) => ({ type: 'em', children: parseInline(m[1]) }))
  // Bare autolink; trailing sentence punctuation is left outside the link.
  // Built directly (not via makeLink) so the URL label is never re-parsed —
  // parsing a bare URL would rediscover this rule and recurse without end.
  add(/https?:\/\/[^\s<>()]*[^\s<>().,;:!?'"]/, (m) => {
    const href = sanitizeUrl(m[0])
    return href === null
      ? { type: 'text', value: m[0] }
      : { type: 'link', href, children: [{ type: 'text', value: m[0] }] }
  })

  return best
}

/** Parses a single line/segment of inline Markdown into an inline AST. */
export function parseInline(text: string): MdInline[] {
  const nodes: MdInline[] = []
  let rest = text
  // Bound the loop defensively: every iteration consumes at least one match or
  // the whole remainder, so this only guards against a pathological zero-width
  // match slipping through a future rule change.
  let guard = 0
  while (rest.length && guard++ < 10_000) {
    const match = firstInline(rest)
    if (!match || match.end <= match.start) {
      pushText(nodes, rest)
      break
    }
    if (match.start > 0) pushText(nodes, rest.slice(0, match.start))
    pushNode(nodes, match.make())
    rest = rest.slice(match.end)
  }
  return nodes
}

/** Joins wrapped paragraph lines, honoring hard breaks (trailing `  ` or `\`). */
function inlineWithBreaks(lines: string[]): MdInline[] {
  const out: MdInline[] = []
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i]
    const hard = /(?: {2,}|\\)$/.test(raw)
    const trimmedEnd = raw.replace(/\s+$/, '')
    // Leading whitespace on continuation lines is insignificant.
    const content = i === 0 ? trimmedEnd : trimmedEnd.replace(/^\s+/, '')
    for (const node of parseInline(content)) pushNode(out, node)
    if (i < lines.length - 1) out.push(hard ? { type: 'br' } : { type: 'text', value: ' ' })
  }
  return out
}

/* ------------------------------------------------------------------ *
 * Block-level detection helpers
 * ------------------------------------------------------------------ */

const RE_FENCE = /^(\s{0,3})(`{3,}|~{3,})\s*([^`]*)$/
const RE_HEADING = /^ {0,3}(#{1,6})(?:\s+(.*?))?\s*(?:\s#+)?\s*$/
const RE_HR = /^ {0,3}([-*_])[ \t]*(?:\1[ \t]*){2,}$/
const RE_QUOTE = /^ {0,3}>/
const RE_LIST = /^(\s*)([-*+]|\d{1,9}[.)])(\s+)(.*)$/

function isTableDelimiter(line: string): boolean {
  return line.includes('-') && /^\s*\|?\s*:?-+:?\s*(?:\|\s*:?-+:?\s*)*\|?\s*$/.test(line)
}

/** True when `line` opens a block that must interrupt an in-progress paragraph. */
function interruptsParagraph(line: string, next: string | undefined): boolean {
  if (RE_FENCE.test(line) || RE_HEADING.test(line) || RE_HR.test(line) || RE_QUOTE.test(line)) return true
  if (RE_LIST.test(line)) return true
  if (line.includes('|') && next !== undefined && isTableDelimiter(next)) return true
  return false
}

function parseTableRow(row: string): string[] {
  let s = row.trim()
  if (s.startsWith('|')) s = s.slice(1)
  if (s.endsWith('|') && !s.endsWith('\\|')) s = s.slice(0, -1)
  const cells: string[] = []
  let cur = ''
  for (let k = 0; k < s.length; k++) {
    const ch = s[k]
    if (ch === '\\' && s[k + 1] === '|') {
      cur += '|'
      k++
      continue
    }
    if (ch === '|') {
      cells.push(cur.trim())
      cur = ''
      continue
    }
    cur += ch
  }
  cells.push(cur.trim())
  return cells
}

function parseAlign(cell: string): MdTableAlign {
  const t = cell.trim()
  const left = t.startsWith(':')
  const right = t.endsWith(':')
  if (left && right) return 'center'
  if (right) return 'right'
  if (left) return 'left'
  return null
}

/* ------------------------------------------------------------------ *
 * Block parsing
 * ------------------------------------------------------------------ */

/** Parses an already line-split document (or nested region) into blocks. */
function parseBlocks(lines: string[]): MdBlock[] {
  const blocks: MdBlock[] = []
  let i = 0

  while (i < lines.length) {
    const line = lines[i]

    if (line.trim() === '') {
      i++
      continue
    }

    // Fenced code block ------------------------------------------------
    const fence = RE_FENCE.exec(line)
    if (fence) {
      const indent = fence[1].length
      const marker = fence[2][0]
      const fenceLen = fence[2].length
      const lang = fence[3].trim() || null
      const closeRe = new RegExp(`^\\s{0,3}\\${marker}{${fenceLen},}\\s*$`)
      const dedent = new RegExp(`^\\s{0,${indent}}`)
      const body: string[] = []
      let closed = false
      i++
      while (i < lines.length) {
        if (closeRe.test(lines[i])) {
          closed = true
          i++
          break
        }
        body.push(lines[i].replace(dedent, ''))
        i++
      }
      blocks.push({ type: 'code', lang, value: body.join('\n'), closed })
      continue
    }

    // ATX heading ------------------------------------------------------
    const heading = RE_HEADING.exec(line)
    if (heading && heading[2] !== undefined) {
      blocks.push({ type: 'heading', level: heading[1].length, children: parseInline(heading[2].trim()) })
      i++
      continue
    }

    // Thematic break (checked before lists: `---` is a rule, `- x` a list) --
    if (RE_HR.test(line)) {
      blocks.push({ type: 'hr' })
      i++
      continue
    }

    // Blockquote -------------------------------------------------------
    if (RE_QUOTE.test(line)) {
      const quoted: string[] = []
      while (i < lines.length && RE_QUOTE.test(lines[i])) {
        quoted.push(lines[i].replace(/^ {0,3}>[ \t]?/, ''))
        i++
      }
      blocks.push({ type: 'blockquote', children: parseBlocks(quoted) })
      continue
    }

    // GFM table --------------------------------------------------------
    if (line.includes('|') && i + 1 < lines.length && isTableDelimiter(lines[i + 1])) {
      const header = parseTableRow(line).map((cell) => parseInline(cell))
      const align = parseTableRow(lines[i + 1]).map(parseAlign)
      const rows: MdInline[][][] = []
      i += 2
      while (i < lines.length && lines[i].trim() !== '' && lines[i].includes('|')) {
        rows.push(parseTableRow(lines[i]).map((cell) => parseInline(cell)))
        i++
      }
      blocks.push({ type: 'table', align, header, rows })
      continue
    }

    // List -------------------------------------------------------------
    if (RE_LIST.test(line)) {
      const result = parseList(lines, i)
      blocks.push(result.block)
      i = result.next
      continue
    }

    // Paragraph --------------------------------------------------------
    const para: string[] = [line]
    i++
    while (i < lines.length && lines[i].trim() !== '' && !interruptsParagraph(lines[i], lines[i + 1])) {
      para.push(lines[i])
      i++
    }
    blocks.push({ type: 'paragraph', children: inlineWithBreaks(para) })
  }

  return blocks
}

/**
 * Parses a run of list items starting at `start`. Continuation and nested lines
 * are recognized by indentation relative to each marker's content column, so
 * paragraphs and sub-lists inside an item are re-parsed recursively.
 */
function parseList(lines: string[], start: number): { block: MdBlock; next: number } {
  const first = RE_LIST.exec(lines[start]) as RegExpExecArray
  const ordered = /\d/.test(first[2])
  const startNum = ordered ? parseInt(first[2], 10) : 1
  const baseIndent = first[1].length
  const items: MdBlock[][] = []
  let i = start

  while (i < lines.length) {
    const marker = RE_LIST.exec(lines[i])
    if (!marker || marker[1].length !== baseIndent) break
    if (/\d/.test(marker[2]) !== ordered) break

    const contentIndent = marker[1].length + marker[2].length + marker[3].length
    const itemLines: string[] = [marker[4]]
    i++

    while (i < lines.length) {
      const l = lines[i]
      if (l.trim() === '') {
        itemLines.push('')
        i++
        continue
      }
      const indent = l.length - l.trimStart().length
      if (indent >= contentIndent) {
        itemLines.push(l.slice(contentIndent))
        i++
      } else {
        break
      }
    }

    while (itemLines.length && itemLines[itemLines.length - 1] === '') itemLines.pop()
    items.push(parseBlocks(itemLines))
  }

  return { block: { type: 'list', ordered, start: startNum, items }, next: i }
}

/** Parses a Markdown document into a block-level AST. */
export function parseMarkdown(source: string): MdBlock[] {
  const normalized = source.replace(/\r\n?/g, '\n').replace(/\t/g, '    ')
  return parseBlocks(normalized.split('\n'))
}
