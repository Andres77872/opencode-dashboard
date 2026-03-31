export interface NavItem {
  label: string
  href: string
  status: 'live' | 'planned'
}

export const navItems: NavItem[] = [
  { label: 'Overview', href: '/overview', status: 'live' },
  { label: 'Daily', href: '/daily', status: 'live' },
  { label: 'Models', href: '/models', status: 'live' },
  { label: 'Tools', href: '/tools', status: 'live' },
  { label: 'Projects', href: '/projects', status: 'live' },
  { label: 'Sessions', href: '/sessions', status: 'live' },
  { label: 'Config', href: '/config', status: 'live' },
]
