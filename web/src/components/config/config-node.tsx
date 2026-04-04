import { isObject } from '../../lib/config-utils'
import type { ConfigJsonValue } from '../../types/config'
import { CollectionValue } from './collection-value'
import { PrimitiveField } from './primitive-field'

interface ConfigNodeProps {
  label: string
  value: ConfigJsonValue
  depth: number
  path: string
  searchActive: boolean
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

export function ConfigNode({ label, value, depth, path, searchActive, copiedId, onCopy }: ConfigNodeProps) {
  if (Array.isArray(value) || isObject(value)) {
    return (
      <CollectionValue
        label={label}
        value={value}
        depth={depth}
        path={path}
        searchActive={searchActive}
        copiedId={copiedId}
        onCopy={onCopy}
        renderNode={({ label: nextLabel, value: nextValue, depth: nextDepth, path: nextPath }) => (
          <ConfigNode
            label={nextLabel}
            value={nextValue}
            depth={nextDepth}
            path={nextPath}
            searchActive={searchActive}
            copiedId={copiedId}
            onCopy={onCopy}
          />
        )}
      />
    )
  }

  return <PrimitiveField label={label} value={value} path={path} copiedId={copiedId} onCopy={onCopy} />
}
