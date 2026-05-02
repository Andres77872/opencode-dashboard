import type { LucideIcon } from 'lucide-react'
import {
  LayoutDashboard,
  Calendar,
  Brain,
  Wrench,
  Folder,
  List,
  Settings,
} from 'lucide-react'

export interface NavItem {
  label: string
  href: string
  icon: LucideIcon
}

export const navItems: NavItem[] = [
  { label: 'Overview', href: '/overview', icon: LayoutDashboard },
  { label: 'Daily', href: '/daily', icon: Calendar },
  { label: 'Models', href: '/models', icon: Brain },
  { label: 'Tools', href: '/tools', icon: Wrench },
  { label: 'Projects', href: '/projects', icon: Folder },
  { label: 'Sessions', href: '/sessions', icon: List },
  { label: 'Config', href: '/config', icon: Settings },
]
