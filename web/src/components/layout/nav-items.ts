import type { IconName } from '../vael/icon'

export interface NavItem {
  label: string
  href: string
  icon: IconName
}

// Order follows the Vael ui_kit sidebar.
export const navItems: NavItem[] = [
  { label: 'Overview', href: '/overview', icon: 'dashboard' },
  { label: 'Daily usage', href: '/daily', icon: 'line-chart' },
  { label: 'Projects', href: '/projects', icon: 'folder' },
  { label: 'Tools', href: '/tools', icon: 'wrench' },
  { label: 'Models', href: '/models', icon: 'cpu' },
  { label: 'Sessions', href: '/sessions', icon: 'clock' },
  { label: 'Config', href: '/config', icon: 'settings' },
]

export interface RouteMeta {
  title: string
  sub: string
}

// Per-route TopBar title + subtitle (mirrors the Vael app.jsx TITLES map).
export const ROUTE_META: Record<string, RouteMeta> = {
  '/overview': { title: 'Overview', sub: 'Usage across all sources' },
  '/daily': { title: 'Daily usage', sub: 'Tokens, cost & sessions per day' },
  '/projects': { title: 'Projects', sub: 'Usage by repository' },
  '/tools': { title: 'Tools', sub: 'What your agents call' },
  '/models': { title: 'Models', sub: 'Per-model usage & cost' },
  '/sessions': { title: 'Sessions', sub: 'Individual agent runs' },
  '/config': { title: 'Config', sub: 'Source, pricing & privacy' },
}

export function routeMeta(pathname: string): RouteMeta {
  const key = Object.keys(ROUTE_META).find((p) => pathname.startsWith(p))
  return (key && ROUTE_META[key]) || { title: 'Vael', sub: '' }
}
