/* Vael top bar — per-route title/subtitle, mobile nav toggle, data freshness,
   database sync and refresh. Fixed flex item (the page body scrolls). */
import { useEffect } from 'react'
import { useLocation } from 'react-router-dom'
import { routeMeta } from './nav-items'
import { IconButton } from '../vael/controls'
import { CacheSyncControl } from './cache-sync'
import { useDashboardContext } from './dashboard-context'
import { useSidebar } from './sidebar-context'
import { formatRelativeTime } from '../../lib/format'

export function TopBar() {
  const { title, sub } = routeMeta(useLocation().pathname)

  // Name the tab after the view, so several dashboard tabs (and the entries
  // in the browser history) can be told apart.
  useEffect(() => {
    document.title = title === 'Vael' ? 'Vael' : `${title} · Vael`
  }, [title])
  const { lastUpdatedAt, isRefreshing, requestRefresh } = useDashboardContext()
  const { mobileOpen, toggleMobile } = useSidebar()

  return (
    <header
      className="vael-bar"
      style={{
        height: 'var(--topbar-height)',
        flexShrink: 0,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        gap: 12,
        borderBottom: '1px solid var(--border-default)',
        background: 'color-mix(in srgb, var(--ink-900) 80%, transparent)',
        backdropFilter: 'blur(8px)',
        position: 'relative',
        zIndex: 'var(--z-topbar)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
        {/* display lives in the class, not an inline style: an inline display
            always beats xl:hidden, which would leave the button on screen at
            desktop widths opening a drawer that CSS hides. */}
        <span className="inline-flex xl:hidden" style={{ marginLeft: -6 }}>
          <IconButton name="menu" label="Open navigation" expanded={mobileOpen} onClick={toggleMobile} />
        </span>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, minWidth: 0 }}>
          <h1 style={{ margin: 0, font: '700 18px/1.2 var(--font-ui)', letterSpacing: '-0.015em', color: 'var(--fg-primary)', whiteSpace: 'nowrap' }}>{title}</h1>
          {sub && <span className="hidden md:inline" style={{ font: '400 13px/1.2 var(--font-ui)', color: 'var(--fg-muted)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{sub}</span>}
        </div>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexShrink: 0 }}>
        <span role="status" style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-muted)', whiteSpace: 'nowrap', marginRight: 4 }}>
          {/* Nothing has been fetched yet on a view that loads no usage data
              (Config opened directly): say nothing instead of inventing a time. */}
          {isRefreshing ? 'Refreshing…' : lastUpdatedAt ? (
            <>
              <span className="hidden sm:inline">Updated </span>
              {formatRelativeTime(lastUpdatedAt)}
            </>
          ) : null}
        </span>
        <CacheSyncControl />
        <IconButton name="refresh" label="Refresh data" onClick={requestRefresh} spinning={isRefreshing} />
      </div>
    </header>
  )
}
