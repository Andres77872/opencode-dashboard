import assert from 'node:assert/strict'
import test from 'node:test'
import {
  isDiagramFence,
  measureText,
  parseMermaid,
  wrapLabel,
  type Diagram,
  type FlowchartDiagram,
  type PieDiagram,
  type SequenceDiagram,
} from './mermaid.ts'

function ok(source: string): { diagram: Diagram; warnings: string[] } {
  const result = parseMermaid(source)
  assert.equal(result.ok, true, `expected a diagram, got ${JSON.stringify(result)}`)
  const success = result as { ok: true; diagram: Diagram; warnings: string[] }
  return { diagram: success.diagram, warnings: success.warnings }
}

function flowchart(source: string): FlowchartDiagram {
  const { diagram } = ok(source)
  assert.equal(diagram.kind, 'flowchart')
  return diagram as FlowchartDiagram
}

function fails(source: string): { error: string; hint: string | null } {
  const result = parseMermaid(source)
  assert.equal(result.ok, false, `expected a failure, got ${JSON.stringify(result)}`)
  const failure = result as { ok: false; error: string; hint: string | null }
  return { error: failure.error, hint: failure.hint }
}

test('isDiagramFence recognizes the diagram fences', () => {
  assert.equal(isDiagramFence('mermaid'), true)
  assert.equal(isDiagramFence('  Mermaid  '), true)
  assert.equal(isDiagramFence('diagram'), true)
  assert.equal(isDiagramFence('chart'), false)
  assert.equal(isDiagramFence(null), false)
})

test('parses a flowchart with shapes, labels, and link styles', () => {
  const diagram = flowchart(`flowchart TD
    A[Browser] --> B{M3 available?}
    B -->|yes| C[(Cache)]
    B -.->|no| D((Stop))
    C ==> E>Report]
  `)
  assert.equal(diagram.direction, 'TD')
  assert.deepEqual(
    diagram.nodes.map((node) => [node.id, node.shape, node.lines.join(' ')]),
    [
      ['A', 'rect', 'Browser'],
      ['B', 'diamond', 'M3 available?'],
      ['C', 'cylinder', 'Cache'],
      ['D', 'circle', 'Stop'],
      ['E', 'rect', 'Report'],
    ],
  )
  assert.equal(diagram.edges.length, 4)
  assert.deepEqual(
    diagram.edges.map((edge) => [edge.from, edge.to, edge.label, edge.dashed, edge.thick]),
    [
      ['A', 'B', null, false, false],
      ['B', 'C', 'yes', false, false],
      ['B', 'D', 'no', true, false],
      ['C', 'E', null, false, true],
    ],
  )
  for (const edge of diagram.edges) {
    assert.match(edge.path, /^M[\d.-]+ [\d.-]+ C/)
    assert.equal(edge.head, 'arrow')
    assert.notEqual(edge.headPoints, '')
  }
})

test('chains, bare references, and open links are all understood', () => {
  const diagram = flowchart(`graph LR
    A[Start] --> B --> C[End]
    A --- C
    B --x C
  `)
  assert.deepEqual(diagram.nodes.map((node) => node.id), ['A', 'B', 'C'])
  assert.deepEqual(diagram.edges.map((edge) => `${edge.from}${edge.to}`), ['AB', 'BC', 'AC', 'BC'])
  assert.equal(diagram.edges[2].head, 'none')
  assert.equal(diagram.edges[3].head, 'cross')
  // A bare reference keeps its id as the label until a declaration provides one.
  assert.deepEqual(diagram.nodes[1].lines, ['B'])
})

test('inline link labels are read from the -- text --> form', () => {
  const diagram = flowchart(`flowchart LR
    A -- retries --> B
    A -. cache miss .-> C
  `)
  assert.deepEqual(diagram.edges.map((edge) => edge.label), ['retries', 'cache miss'])
  assert.equal(diagram.edges[1].dashed, true)
})

test('layout places ranks along the direction axis and stays inside the viewBox', () => {
  const down = flowchart('flowchart TD\n A[One] --> B[Two]')
  assert.ok(down.nodes[1].y > down.nodes[0].y, 'TD should place the target below the source')
  assert.equal(down.nodes[0].x, down.nodes[1].x)

  const right = flowchart('flowchart LR\n A[One] --> B[Two]')
  assert.ok(right.nodes[1].x > right.nodes[0].x, 'LR should place the target to the right')
  assert.equal(right.nodes[0].y, right.nodes[1].y)

  const up = flowchart('flowchart BT\n A[One] --> B[Two]')
  assert.ok(up.nodes[1].y < up.nodes[0].y, 'BT should place the target above the source')

  for (const diagram of [down, right, up]) {
    for (const node of diagram.nodes) {
      assert.ok(node.x - node.w / 2 >= 0, 'node overflows the left edge')
      assert.ok(node.y - node.h / 2 >= 0, 'node overflows the top edge')
      assert.ok(node.x + node.w / 2 <= diagram.width, 'node overflows the right edge')
      assert.ok(node.y + node.h / 2 <= diagram.height, 'node overflows the bottom edge')
    }
  }
})

test('a cyclic graph terminates with a usable layering', () => {
  const diagram = flowchart('flowchart TD\n A --> B\n B --> C\n C --> A')
  assert.equal(diagram.nodes.length, 3)
  assert.equal(diagram.edges.length, 3)
  assert.ok(diagram.height > 0)
})

test('styling, interaction, and subgraph directives warn instead of drawing', () => {
  const withStyle = ok(`flowchart TD
    A[One] --> B[Two]
    style A fill:#f00
    click A "https://example.com"
  `)
  assert.equal(withStyle.warnings.length, 1)
  assert.match(withStyle.warnings[0], /Styling and interaction directives are ignored/)

  const withSubgraph = ok(`flowchart TD
    subgraph backend
    A --> B
    end
  `)
  assert.match(withSubgraph.warnings[0], /Subgraph grouping is not drawn/)
  assert.equal((withSubgraph.diagram as FlowchartDiagram).nodes.length, 2)
})

test('init directives and comments are dropped entirely', () => {
  const diagram = flowchart(`flowchart TD
    %%{init: {"securityLevel": "loose"}}%%
    %% a comment
    A[One] --> B[Two]
  `)
  assert.equal(diagram.nodes.length, 2)
})

test('parses a sequence diagram with participants, replies, and notes', () => {
  const { diagram } = ok(`sequenceDiagram
    participant W as Web chat
    participant B as Backend
    W->>B: POST /chat/stream
    B-->>W: content deltas
    Note over W,B: bounded to 90s
  `)
  assert.equal(diagram.kind, 'sequence')
  const sequence = diagram as SequenceDiagram
  assert.deepEqual(sequence.actors.map((actor) => actor.label), ['Web chat', 'Backend'])
  assert.equal(sequence.messages.length, 2)
  assert.equal(sequence.messages[0].dashed, false)
  assert.equal(sequence.messages[1].dashed, true)
  assert.ok(sequence.messages[1].y > sequence.messages[0].y)
  assert.equal(sequence.notes.length, 1)
  assert.deepEqual(sequence.notes[0].lines, ['bounded to 90s'])
  assert.ok(sequence.lifelineBottom > sequence.lifelineTop)
})

test('a self-call is marked so the renderer can draw the loop', () => {
  const { diagram } = ok('sequenceDiagram\n A->>A: retry once')
  const sequence = diagram as SequenceDiagram
  assert.equal(sequence.messages[0].selfCall, true)
})

test('sequence grouping blocks warn rather than dropping their messages', () => {
  const { diagram, warnings } = ok(`sequenceDiagram
    loop every round
    A->>B: call tool
    end
  `)
  assert.match(warnings[0], /Grouping blocks/)
  assert.equal((diagram as SequenceDiagram).messages.length, 1)
})

test('parses a pie diagram into slices', () => {
  const { diagram } = ok(`pie showData title Requests by model
    "kimi-k2" : 62
    "kimi-k2-turbo" : 38
  `)
  assert.equal(diagram.kind, 'pie')
  const pie = diagram as PieDiagram
  assert.equal(pie.title, 'Requests by model')
  assert.deepEqual(pie.slices, [
    { label: 'kimi-k2', value: 62 },
    { label: 'kimi-k2-turbo', value: 38 },
  ])
})

test('unsupported syntax fails with an actionable diagnostic', () => {
  assert.match(fails('').error, /empty/)
  assert.match(fails('classDiagram\n A --|> B').error, /not a supported diagram type/)
  assert.match(String(fails('classDiagram').hint), /flowchart .*sequenceDiagram.*pie/)
  assert.match(fails('flowchart TD\n A & B --> C').error, /"&" are not supported/)
  assert.match(fails('flowchart TD\n A -->').error, /no target/)
  assert.match(fails('flowchart TD').error, /declares no nodes/)
  assert.match(fails('sequenceDiagram\n this is prose').error, /not a participant, message, or note/)
  assert.match(fails('pie\n not a slice').error, /not a pie slice/)
})

test('size limits are enforced', () => {
  const nodes = Array.from({ length: 41 }, (_, i) => `  N${i}[Node ${i}]`).join('\n')
  assert.match(fails(`flowchart TD\n${nodes}`).error, /more than 40 nodes/)

  const slices = Array.from({ length: 9 }, (_, i) => `  "s${i}" : ${i + 1}`).join('\n')
  assert.match(fails(`pie\n${slices}`).error, /9 slices; the limit is 8/)
})

test('labels are scrubbed, unwrapped, and bounded', () => {
  const diagram = flowchart('flowchart TD\n A["A‮B"] --> B["  spaced   label  "]')
  assert.deepEqual(diagram.nodes[0].lines, ['A B'])
  assert.deepEqual(diagram.nodes[1].lines, ['spaced label'])

  const long = flowchart(`flowchart TD\n A[${'word '.repeat(30)}] --> B[x]`)
  assert.ok(long.nodes[0].lines.length <= 3)
  assert.ok(long.nodes[0].w <= 190)
})

test('text measurement and wrapping are deterministic', () => {
  assert.ok(measureText('MMMM') > measureText('iiii'))
  assert.equal(measureText(''), 0)
  assert.deepEqual(wrapLabel('one two three four five', 9), ['one two', 'three', 'four five'])
  // A hard break keeps the hyphen inside the budget: 7 characters plus "-".
  assert.deepEqual(wrapLabel('supercalifragilistic', 8), ['superca-', 'lifragi-', 'listic'])
  assert.deepEqual(wrapLabel(''), [''])
})
