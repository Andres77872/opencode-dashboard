import assert from 'node:assert/strict'
import test from 'node:test'
import { conversationToMarkdown } from './assistant-transcript.ts'

test('a conversation is emitted as labelled Markdown sections', () => {
  assert.equal(
    conversationToMarkdown([
      { role: 'user', content: 'Summarize my usage.' },
      { role: 'assistant', content: '## Usage\n\n- 1,284 sessions' },
      { role: 'user', content: 'Break it down by model.' },
    ]),
    [
      '## You',
      '',
      'Summarize my usage.',
      '',
      '## Analytics assistant',
      '',
      '## Usage\n\n- 1,284 sessions',
      '',
      '## You',
      '',
      'Break it down by model.',
    ].join('\n'),
  )
})

test('Markdown source is copied verbatim, not re-serialized', () => {
  const source = '| model | cost |\n| --- | --- |\n| **opus** | `$4.20` |'
  assert.equal(
    conversationToMarkdown([{ role: 'assistant', content: source }]),
    `## Analytics assistant\n\n${source}`,
  )
})

test('turns that produced nothing are skipped', () => {
  assert.equal(
    conversationToMarkdown([
      { role: 'user', content: 'Hello' },
      { role: 'assistant', content: '   ' },
    ]),
    '## You\n\nHello',
  )
  assert.equal(conversationToMarkdown([]), '')
})
