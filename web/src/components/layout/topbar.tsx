/* Vael top bar — per-route title/subtitle, mobile nav toggle, live sync status
   + refresh. Sticky-fixed flex item (the in-page scroll lives in PageBody). */
import { useLocation } from 'react-router-dom'
import { routeMeta } from './nav-items'
import { IconButton } from '../vael/controls'
import { Icon } from '../vael/icon'
import { useDashboardContext } from './dashboard-context'
import { useSidebar } from './sidebar-context'
import { formatRelativeTime } from '../../lib/format'

export function TopBar() {
  const { title, sub } = routeMeta(useLocation().pathname)
  const { lastUpdatedAt, isRefreshing, requestRefresh } = useDashboardContext()
  const { toggleMobile } = useSidebar()

  return (
    <header
      style={{
        height: 'var(--topbar-height)',
        flexShrink: 0,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        gap: 12,
        padding: '0 24px',
        borderBottom: '1px solid var(--border-default)',
        background: 'color-mix(in srgb, var(--ink-900) 80%, transparent)',
        backdropFilter: 'blur(8px)',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, minWidth: 0 }}>
        <button
          type="button"
          aria-label="Open navigation"
          onClick={toggleMobile}
          className="xl:hidden"
          style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 32, height: 32, marginLeft: -6, color: 'var(--fg-muted)', background: 'transparent', border: '1px solid transparent', borderRadius: 'var(--radius-md)', cursor: 'pointer', flexShrink: 0 }}
        >
          <Icon name="menu" size={18} />
        </button>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 10, minWidth: 0 }}>
          <h1 style={{ margin: 0, font: '700 18px/1.2 var(--font-ui)', letterSpacing: '-0.015em', color: 'var(--fg-primary)', whiteSpace: 'nowrap' }}>{title}</h1>
          {sub && <span className="hidden sm:inline" style={{ font: '400 13px/1 var(--font-ui)', color: 'var(--fg-muted)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{sub}</span>}
        </div>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 10, flexShrink: 0 }}>
        <span style={{ font: '400 12px/1 var(--font-ui)', color: 'var(--fg-muted)', whiteSpace: 'nowrap' }}>
          <span className="hidden sm:inline">Last sync </span>
          {isRefreshing ? 'syncing…' : formatRelativeTime(lastUpdatedAt)}
        </span>
        <IconButton name="refresh" label="Refresh data" onClick={requestRefresh} spinning={isRefreshing} />
      </div>
    </header>
  )
}
