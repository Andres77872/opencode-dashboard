import { NavLink } from 'react-router-dom'
import { navItems } from './nav-items'
import { Badge } from '../ui/badge'
import { cn } from '../../lib/utils'

interface PrimaryNavProps {
  orientation: 'horizontal' | 'vertical'
}

export function PrimaryNav({ orientation }: PrimaryNavProps) {
  return (
    <nav
      aria-label="Primary"
      className={cn(
        orientation === 'horizontal'
          ? 'overflow-x-auto px-4 py-3 sm:px-6 xl:hidden'
          : 'hidden xl:block xl:w-64 xl:flex-none',
      )}
    >
      <div
        className={cn(
          orientation === 'horizontal'
            ? 'flex min-w-max items-center gap-2'
            : 'sticky top-[116px] flex flex-col gap-2 rounded-2xl border border-border/70 bg-panel/75 p-3',
        )}
      >
        {navItems.map((item) => (
          <NavLink
            key={item.href}
            to={item.href}
            className={({ isActive }) =>
              cn(
                'group flex items-center justify-between gap-3 rounded-xl border px-3 py-2 text-sm transition-colors',
                orientation === 'horizontal' ? 'min-w-[152px]' : 'w-full',
                isActive
                  ? 'border-accent/45 bg-accent/12 text-foreground'
                  : 'border-transparent bg-transparent text-muted-foreground hover:border-border/70 hover:bg-white/4 hover:text-foreground',
              )
            }
          >
            {({ isActive }) => (
              <>
                <span className="font-medium">{item.label}</span>
                <Badge tone={item.status === 'live' || isActive ? 'accent' : 'default'}>
                  {item.status === 'live' ? 'Live' : 'Soon'}
                </Badge>
              </>
            )}
          </NavLink>
        ))}
      </div>
    </nav>
  )
}
