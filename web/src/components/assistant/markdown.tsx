/**
 * Renders the analytics assistant's Markdown responses. Parsing lives in
 * lib/markdown (unit-tested there); this file is purely the AST → React mapping.
 *
 * Everything is real DOM — no dangerouslySetInnerHTML — and links carry a
 * scheme allowlist plus noopener, so a model response can never inject markup or
 * reach window.opener. Styling is class-based and scoped to
 * `.analytics-assistant-md` in index.css.
 */
import { Fragment, useMemo, type ReactNode } from 'react'
import { parseMarkdown, type MdBlock, type MdInline } from '../../lib/markdown'
import { CopyButton } from './copy-button'

function InlineNodes({ nodes }: { nodes: MdInline[] }): ReactNode {
  return (
    <>
      {nodes.map((node, index) => (
        <InlineNode key={index} node={node} />
      ))}
    </>
  )
}

function InlineNode({ node }: { node: MdInline }): ReactNode {
  switch (node.type) {
    case 'text':
      return node.value
    case 'strong':
      return <strong><InlineNodes nodes={node.children} /></strong>
    case 'em':
      return <em><InlineNodes nodes={node.children} /></em>
    case 'del':
      return <del><InlineNodes nodes={node.children} /></del>
    case 'code':
      return <code className="md-inline-code">{node.value}</code>
    case 'br':
      return <br />
    case 'link':
      return (
        <a href={node.href} target="_blank" rel="noopener noreferrer nofollow">
          <InlineNodes nodes={node.children} />
        </a>
      )
  }
}

function CodeBlock({ lang, value }: { lang: string | null; value: string }) {
  return (
    <div className="md-codeblock">
      <div className="md-codeblock-head">
        <span className="md-codeblock-lang">{lang || 'text'}</span>
        <CopyButton value={value} label="Copy" className="md-codeblock-copy" />
      </div>
      <pre>
        <code>{value}</code>
      </pre>
    </div>
  )
}

/** Renders the blocks of a single list item, keeping a leading paragraph tight. */
function ItemContent({ blocks }: { blocks: MdBlock[] }): ReactNode {
  return (
    <>
      {blocks.map((block, index) =>
        block.type === 'paragraph' ? (
          <Fragment key={index}>
            <InlineNodes nodes={block.children} />
          </Fragment>
        ) : (
          <BlockNode key={index} block={block} />
        ),
      )}
    </>
  )
}

function StreamCursor() {
  return <span className="analytics-assistant-stream-cursor" aria-hidden="true" />
}

function BlockNode({ block, trailing }: { block: MdBlock; trailing?: ReactNode }): ReactNode {
  switch (block.type) {
    case 'heading': {
      const Tag = `h${block.level}` as 'h1'
      return (
        <Tag className={`md-h md-h${block.level}`}>
          <InlineNodes nodes={block.children} />
          {trailing}
        </Tag>
      )
    }
    case 'paragraph':
      return <p className="md-p"><InlineNodes nodes={block.children} />{trailing}</p>
    case 'code':
      return <CodeBlock lang={block.lang} value={block.value} />
    case 'hr':
      return <hr className="md-hr" />
    case 'blockquote':
      return (
        <blockquote className="md-quote">
          {block.children.map((child, index) => (
            <BlockNode key={index} block={child} />
          ))}
        </blockquote>
      )
    case 'list': {
      if (block.ordered) {
        return (
          <ol className="md-list" start={block.start}>
            {block.items.map((item, index) => (
              <li key={index}><ItemContent blocks={item} /></li>
            ))}
          </ol>
        )
      }
      return (
        <ul className="md-list">
          {block.items.map((item, index) => (
            <li key={index}><ItemContent blocks={item} /></li>
          ))}
        </ul>
      )
    }
    case 'table': {
      const columns = block.header.length
      return (
        <div className="md-table-wrap">
          <table className="md-table">
            <thead>
              <tr>
                {block.header.map((cell, index) => (
                  <th key={index} style={{ textAlign: block.align[index] ?? undefined }}>
                    <InlineNodes nodes={cell} />
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {block.rows.map((row, rowIndex) => (
                <tr key={rowIndex}>
                  {Array.from({ length: columns }, (_, colIndex) => (
                    <td key={colIndex} style={{ textAlign: block.align[colIndex] ?? undefined }}>
                      <InlineNodes nodes={row[colIndex] ?? []} />
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )
    }
  }
}

/**
 * Renders a Markdown string as sanitized, styled React content. When
 * `streaming` is set, a blinking caret is appended — inline at the end of the
 * final paragraph/heading when possible, otherwise as a trailing block — so a
 * partially received response reads as still in progress.
 */
export function Markdown({ content, streaming = false }: { content: string; streaming?: boolean }) {
  const blocks = useMemo(() => parseMarkdown(content), [content])
  const lastIndex = blocks.length - 1
  const inlineCaretTarget = blocks[lastIndex]?.type === 'paragraph' || blocks[lastIndex]?.type === 'heading'
  return (
    <div className="analytics-assistant-md">
      {blocks.map((block, index) => (
        <BlockNode
          key={index}
          block={block}
          trailing={streaming && inlineCaretTarget && index === lastIndex ? <StreamCursor /> : undefined}
        />
      ))}
      {streaming && !inlineCaretTarget && <StreamCursor />}
    </div>
  )
}
