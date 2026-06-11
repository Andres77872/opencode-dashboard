/* Vael sidebar — constellation logo + Vael wordmark, nav rail, live status footer.
   Desktop: fixed 232px rail. Mobile: slide-over drawer (via sidebar-context). */
import { useEffect } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import type { CSSProperties } from 'react'
import { navItems } from './nav-items'
import { Icon } from '../vael/icon'
import { useDashboardContext } from './dashboard-context'
import { useSidebar } from './sidebar-context'
import { formatRelativeTime } from '../../lib/format'

// Global filters that follow the user between views; view-specific params are dropped.
const GLOBAL_PARAM_KEYS = ['source', 'period', 'from', 'to', 'mode'] as const

function globalSearch(search: string): string {
  const current = new URLSearchParams(search)
  const next = new URLSearchParams()
  for (const key of GLOBAL_PARAM_KEYS) {
    const value = current.get(key)
    if (value !== null) next.set(key, value)
  }
  const serialized = next.toString()
  return serialized ? `?${serialized}` : ''
}

const Logo = () => (
  <svg width="26" height="26" viewBox="0 0 40 40" fill="none">
    <path d="M9 9.5 L20 30 L31 9.5" stroke="#4d9fff" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" opacity="0.55" />
    <circle cx="9" cy="9.5" r="3" fill="#8fc1ff" />
    <circle cx="20" cy="30" r="3.4" fill="#4d9fff" />
    <circle cx="31" cy="9.5" r="3" fill="#8fc1ff" />
    <circle cx="29.5" cy="20" r="1.4" fill="#aab3c0" opacity="0.7" />
  </svg>
)

function navItemStyle(isActive: boolean): CSSProperties {
  return {
    position: 'relative',
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    width: '100%',
    height: 34,
    padding: '0 11px',
    borderRadius: 'var(--radius-md)',
    background: isActive ? 'var(--accent-soft)' : 'transparent',
    color: isActive ? 'var(--fg-primary)' : 'var(--fg-secondary)',
    font: `${isActive ? 600 : 500} 13px/1 var(--font-ui)`,
    cursor: 'pointer',
    textAlign: 'left',
    transition: 'background var(--dur-fast)',
  }
}

function NavList({ search, onNavigate }: { search: string; onNavigate?: () => void }) {
  return (
    <nav style={{ padding: '10px', flex: 1, display: 'flex', flexDirection: 'column', gap: 1, overflowY: 'auto' }}>
      <div style={{ font: '600 10px/1 var(--font-ui)', letterSpacing: '0.08em', textTransform: 'uppercase', color: 'var(--fg-faint)', padding: '8px 11px 6px' }}>Analyze</div>
      {navItems.map((item) => (
        <NavLink
          key={item.href}
          to={{ pathname: item.href, search }}
          onClick={onNavigate}
          style={({ isActive }) => navItemStyle(isActive)}
        >
          {({ isActive }) => (
            <>
              {isActive && <span style={{ position: 'absolute', left: -1, top: 8, bottom: 8, width: 3, borderRadius: 2, background: 'var(--accent)' }} />}
              <Icon name={item.icon} size={18} color={isActive ? 'var(--accent)' : 'var(--fg-muted)'} />
              <span style={{ flex: 1 }}>{item.label}</span>
            </>
          )}
        </NavLink>
      ))}
    </nav>
  )
}

function StatusFooter() {
  const { lastUpdatedAt, isRefreshing, requestRefresh } = useDashboardContext()
  return (
    <div style={{ padding: 10, borderTop: '1px solid var(--border-subtle)' }}>
      <button
        type="button"
        onClick={requestRefresh}
        title="Refresh data"
        style={{ display: 'flex', alignItems: 'center', gap: 9, width: '100%', padding: '8px 10px', borderRadius: 'var(--radius-md)', background: 'var(--ink-800)', border: '1px solid var(--border-subtle)', cursor: 'pointer', textAlign: 'left' }}
      >
        <span style={{ width: 8, height: 8, borderRadius: '50%', background: 'var(--success)', boxShadow: 'var(--glow-success)', flexShrink: 0 }} />
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ font: '600 12px/1.2 var(--font-ui)', color: 'var(--fg-primary)' }}>Local agent</div>
          <div style={{ font: '400 11px/1 var(--font-mono)', color: 'var(--fg-muted)', marginTop: 3 }}>{isRefreshing ? 'refreshing...' : `refreshed ${formatRelativeTime(lastUpdatedAt)}`}</div>
        </div>
        <Icon name="refresh" size={14} color="var(--fg-muted)" style={isRefreshing ? { animation: 'vael-spin 0.9s linear infinite' } : undefined} />
      </button>
    </div>
  )
}

const BrandRow = () => (
  <div style={{ height: 'var(--topbar-height)', display: 'flex', alignItems: 'center', gap: 9, padding: '0 16px', borderBottom: '1px solid var(--border-subtle)', flexShrink: 0 }}>
    <Logo />
    <span style={{ font: '700 18px/1 var(--font-ui)', letterSpacing: '-0.02em', color: 'var(--fg-primary)' }}>Vael</span>
  </div>
)

export function Sidebar() {
  const { mobileOpen, closeMobile } = useSidebar()
  const search = globalSearch(useLocation().search)

  useEffect(() => {
    if (!mobileOpen) return
    document.body.style.overflow = 'hidden'
    return () => {
      document.body.style.overflow = ''
    }
  }, [mobileOpen])

  return (
    <>
      {/* Desktop rail */}
      <aside
        className="hidden xl:flex"
        style={{ width: 'var(--rail-width)', flexShrink: 0, background: 'var(--ink-850)', borderRight: '1px solid var(--border-default)', flexDirection: 'column', height: '100%' }}
      >
        <BrandRow />
        <NavList search={search} />
        <StatusFooter />
      </aside>

      {/* Mobile drawer */}
      {mobileOpen && (
        <div className="xl:hidden" style={{ position: 'fixed', inset: 0, zIndex: 90 }}>
          <div onClick={closeMobile} style={{ position: 'absolute', inset: 0, background: 'rgba(0,0,0,0.55)' }} />
          <aside
            style={{
              position: 'absolute',
              left: 0,
              top: 0,
              height: '100%',
              width: 'var(--rail-width)',
              background: 'var(--ink-850)',
              borderRight: '1px solid var(--border-default)',
              display: 'flex',
              flexDirection: 'column',
              boxShadow: 'var(--shadow-xl)',
              animation: 'vael-slide-in-right var(--dur-base) var(--ease-out)',
            }}
          >
            <BrandRow />
            <NavList search={search} onNavigate={closeMobile} />
            <StatusFooter />
          </aside>
        </div>
      )}
    </>
  )
}
