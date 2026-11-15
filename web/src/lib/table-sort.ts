export type SortDirection = 'asc' | 'desc'

export interface SortState<SortKey extends string> {
  key: SortKey
  direction: SortDirection
}

function reverseDirection(direction: SortDirection): SortDirection {
  return direction === 'asc' ? 'desc' : 'asc'
}

export function getNextSortState<SortKey extends string>(
  current: SortState<SortKey> | null,
  key: SortKey,
  defaultDirection: SortDirection,
): SortState<SortKey> | null {
  if (!current || current.key !== key) {
    return { key, direction: defaultDirection }
  }

  if (current.direction === defaultDirection) {
    return { key, direction: reverseDirection(defaultDirection) }
  }

  return null
}
