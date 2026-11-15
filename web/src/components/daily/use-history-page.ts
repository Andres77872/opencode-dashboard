import { useCallback, useEffect, useState } from 'react'
import { useNavigationType, useSearchParams } from 'react-router-dom'

function parsePage(raw: string | null) {
  const page = Number.parseInt(raw ?? '', 10)
  return Number.isFinite(page) && page >= 1 ? page : 1
}

export interface HistoryPage {
  /** 1-based page to show. */
  page: number
  /** The user moved to another page: recorded as a history entry. */
  goToPage: (page: number) => void
  /** The page was wrong for the data (new ordering, past the end): fixed in place, no history entry. */
  correctPage: (page: number) => void
}

/**
 * The message history's page number, kept in the URL (?page=) so a position
 * can be shared and survives back/forward.
 *
 * `scope` names what is being paged (source + range). A page chosen for one
 * scope does not carry over to the next, so a scope change starts again at
 * page 1 — decided while rendering, so the old page is never requested for
 * the new scope. Back/forward is the exception: there the URL records what
 * was on screen and is restored as written.
 *
 * Call it from a component that stays mounted across scope changes; the list
 * itself is replaced by a skeleton while a new scope loads.
 */
export function useHistoryPage(scope: string): HistoryPage {
  const [searchParams, setSearchParams] = useSearchParams()
  const navigationType = useNavigationType()
  const urlPage = parsePage(searchParams.get('page'))

  const [page, setPage] = useState(urlPage)
  const [seen, setSeen] = useState({ scope, urlPage })

  if (seen.scope !== scope || seen.urlPage !== urlPage) {
    setSeen({ scope, urlPage })
    setPage(seen.scope !== scope && navigationType !== 'POP' ? 1 : urlPage)
  }

  const writePage = useCallback(
    (next: number, replace: boolean) => {
      setSearchParams(
        (previous) => {
          const params = new URLSearchParams(previous)
          if (next <= 1) params.delete('page')
          else params.set('page', String(next))
          return params
        },
        { replace },
      )
    },
    [setSearchParams],
  )

  // The page can change here without a navigation (reset for a new scope,
  // corrected by the list). Bring the URL along without adding a history entry.
  useEffect(() => {
    if (page !== urlPage) writePage(page, true)
  }, [page, urlPage, writePage])

  const goToPage = useCallback((next: number) => writePage(next, false), [writePage])

  return { page, goToPage, correctPage: setPage }
}
