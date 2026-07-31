import assert from 'node:assert/strict'
import test from 'node:test'
import {
  parseInline,
  parseMarkdown,
  sanitizeUrl,
  type MdBlock,
  type MdInline,
} from './markdown.ts'

function only<T extends MdBlock['type']>(md: string, type: T): Extract<MdBlock, { type: T }> {
  const blocks = parseMarkdown(md)
  assert.equal(blocks.length, 1, `expected one block, got ${JSON.stringify(blocks)}`)
  assert.equal(blocks[0].type, type)
  return blocks[0] as Extract<MdBlock, { type: T }>
}

function text(nodes: MdInline[]): string {
  return nodes
    .map((n) => {
      switch (n.type) {
        case 'text':
          return n.value
        case 'code':
          return n.value
        case 'br':
          return '\n'
        default:
          return text(n.children)
      }
    })
    .join('')
}

test('sanitizeUrl allows safe schemes and relative links', () => {
  assert.equal(sanitizeUrl('https://example.com'), 'https://example.com')
  assert.equal(sanitizeUrl('http://example.com'), 'http://example.com')
  assert.equal(sanitizeUrl('mailto:a@b.com'), 'mailto:a@b.com')
  assert.equal(sanitizeUrl('/overview'), '/overview')
  assert.equal(sanitizeUrl('#anchor'), '#anchor')
  assert.equal(sanitizeUrl('docs/guide'), 'docs/guide')
})

test('sanitizeUrl rejects dangerous schemes', () => {
  assert.equal(sanitizeUrl('javascript:alert(1)'), null)
  assert.equal(sanitizeUrl('  javascript:alert(1)'), null)
  assert.equal(sanitizeUrl('data:text/html,<script>'), null)
  assert.equal(sanitizeUrl('vbscript:msgbox'), null)
  assert.equal(sanitizeUrl(''), null)
})

test('parses ATX headings with levels', () => {
  const h = only('## Usage summary', 'heading')
  assert.equal(h.level, 2)
  assert.equal(text(h.children), 'Usage summary')

  const h6 = only('###### Deep', 'heading')
  assert.equal(h6.level, 6)

  // seven hashes is not a heading
  const p = only('####### Not a heading', 'paragraph')
  assert.equal(p.type, 'paragraph')
})

test('parses bold, italic, and strikethrough', () => {
  const p = only('This is **bold**, *italic*, and ~~gone~~.', 'paragraph')
  const kinds = p.children.map((n) => n.type)
  assert.ok(kinds.includes('strong'))
  assert.ok(kinds.includes('em'))
  assert.ok(kinds.includes('del'))
})

test('bold is not misread as nested emphasis', () => {
  const p = only('**bold**', 'paragraph')
  assert.equal(p.children.length, 1)
  assert.equal(p.children[0].type, 'strong')
  assert.equal(text(p.children), 'bold')
})

test('underscore emphasis fires at word boundaries but not inside identifiers', () => {
  const identifier = only('Call get_daily_usage now.', 'paragraph')
  assert.equal(identifier.children.every((n) => n.type === 'text'), true)
  assert.equal(text(identifier.children), 'Call get_daily_usage now.')

  const emphasized = only('An _italic_ word.', 'paragraph')
  assert.ok(emphasized.children.some((n) => n.type === 'em'))
})

test('parses inline code verbatim without emphasis inside', () => {
  const p = only('Run `get_daily_usage --flag` please', 'paragraph')
  const code = p.children.find((n) => n.type === 'code')
  assert.ok(code)
  assert.equal(code?.type === 'code' ? code.value : '', 'get_daily_usage --flag')
})

test('parses fenced code blocks with a language', () => {
  const block = only('```json\n{"a": 1}\n```', 'code')
  assert.equal(block.lang, 'json')
  assert.equal(block.value, '{"a": 1}')
  assert.equal(block.closed, true)
})

test('unterminated fence still yields a code block (streaming safety)', () => {
  const block = only('```python\nprint(1)', 'code')
  assert.equal(block.lang, 'python')
  assert.equal(block.value, 'print(1)')
  // Artifact fences render a placeholder until this flips to true, so a
  // half-received chart spec never renders as a parse failure.
  assert.equal(block.closed, false)
})

test('an artifact fence keeps its type argument in the info string', () => {
  const block = only('```chart bar\n{"data":[]}\n```', 'code')
  assert.equal(block.lang, 'chart bar')
  assert.equal(block.closed, true)
})

test('unterminated emphasis degrades to literal text', () => {
  const p = only('This is **not closed', 'paragraph')
  assert.equal(text(p.children), 'This is **not closed')
})

test('parses safe links and drops javascript links to text', () => {
  const safe = only('See [the docs](https://example.com).', 'paragraph')
  const link = safe.children.find((n) => n.type === 'link')
  assert.ok(link)
  assert.equal(link?.type === 'link' ? link.href : '', 'https://example.com')

  const unsafe = only('Click [here](javascript:alert(1)).', 'paragraph')
  assert.equal(unsafe.children.some((n) => n.type === 'link'), false)
  assert.ok(text(unsafe.children).includes('here'))
})

test('parses bare autolinks and leaves trailing punctuation out', () => {
  const p = only('Visit https://example.com/x, then leave.', 'paragraph')
  const link = p.children.find((n) => n.type === 'link')
  assert.ok(link)
  assert.equal(link?.type === 'link' ? link.href : '', 'https://example.com/x')
  assert.ok(text(p.children).includes(','))
})

test('parses unordered lists', () => {
  const list = only('- one\n- two\n- three', 'list')
  assert.equal(list.ordered, false)
  assert.equal(list.items.length, 3)
  assert.equal(text((list.items[0][0] as Extract<MdBlock, { type: 'paragraph' }>).children), 'one')
})

test('parses ordered lists with a start offset', () => {
  const list = only('3. third\n4. fourth', 'list')
  assert.equal(list.ordered, true)
  assert.equal(list.start, 3)
  assert.equal(list.items.length, 2)
})

test('parses nested lists by indentation', () => {
  const list = only('- parent\n  - child\n  - child two', 'list')
  assert.equal(list.items.length, 1)
  const inner = list.items[0].find((b) => b.type === 'list')
  assert.ok(inner)
  assert.equal(inner?.type === 'list' ? inner.items.length : 0, 2)
})

test('parses blockquotes recursively', () => {
  const quote = only('> **Note:** watch spend.', 'blockquote')
  assert.equal(quote.children.length, 1)
  assert.equal(quote.children[0].type, 'paragraph')
})

test('parses thematic breaks', () => {
  const blocks = parseMarkdown('above\n\n---\n\nbelow')
  assert.deepEqual(blocks.map((b) => b.type), ['paragraph', 'hr', 'paragraph'])
})

test('parses GFM tables with alignment', () => {
  const md = '| Model | Tokens |\n| :--- | ---: |\n| opus | 1,024 |\n| haiku | 512 |'
  const table = only(md, 'table')
  assert.equal(table.header.length, 2)
  assert.deepEqual(table.align, ['left', 'right'])
  assert.equal(table.rows.length, 2)
  assert.equal(text(table.rows[0][0]), 'opus')
  assert.equal(text(table.rows[1][1]), '512')
})

test('renders inline markup inside table cells', () => {
  const md = '| Name | Status |\n| --- | --- |\n| `job_a` | **ok** |'
  const table = only(md, 'table')
  assert.equal(table.rows[0][0][0].type, 'code')
  assert.equal(table.rows[0][1][0].type, 'strong')
})

test('separates paragraphs on blank lines', () => {
  const blocks = parseMarkdown('First paragraph.\n\nSecond paragraph.')
  assert.deepEqual(blocks.map((b) => b.type), ['paragraph', 'paragraph'])
})

test('a list interrupts an open paragraph', () => {
  const blocks = parseMarkdown('Here are the sources:\n- opencode\n- codex')
  assert.deepEqual(blocks.map((b) => b.type), ['paragraph', 'list'])
})

test('joins wrapped lines with spaces and honors hard breaks', () => {
  const soft = only('one\ntwo', 'paragraph')
  assert.equal(text(soft.children), 'one two')

  const hard = only('one  \ntwo', 'paragraph')
  assert.ok(hard.children.some((n) => n.type === 'br'))
})

test('empty input yields no blocks', () => {
  assert.deepEqual(parseMarkdown(''), [])
  assert.deepEqual(parseMarkdown('\n\n'), [])
})

test('parseInline is exported for cell/segment reuse', () => {
  const nodes = parseInline('a **b** c')
  assert.equal(text(nodes), 'a b c')
  assert.ok(nodes.some((n) => n.type === 'strong'))
})
