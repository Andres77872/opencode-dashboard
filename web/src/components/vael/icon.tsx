/* Icon — inline Lucide-style paths (ported from the Vael ui_kit), so React
   re-renders never wipe them and no icon font/runtime dependency is needed. */
import type { CSSProperties } from 'react'

export const ICON_PATHS = {
  'dashboard': '<rect x="3" y="3" width="7" height="9" rx="1"/><rect x="14" y="3" width="7" height="5" rx="1"/><rect x="14" y="12" width="7" height="9" rx="1"/><rect x="3" y="16" width="7" height="5" rx="1"/>',
  'line-chart': '<path d="M3 3v18h18"/><path d="M7 14l3-3 3 3 5-6"/>',
  'bar-chart': '<path d="M3 3v18h18"/><rect x="7" y="11" width="3" height="6" rx="0.5"/><rect x="12" y="7" width="3" height="10" rx="0.5"/><rect x="17" y="13" width="3" height="4" rx="0.5"/>',
  'folder': '<path d="M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2z"/>',
  'wrench': '<path d="M14.7 6.3a4 4 0 00-5.4 5.4L3 18v3h3l6.3-6.3a4 4 0 005.4-5.4l-2.7 2.7-2-2 2.7-2.7z"/>',
  'clock': '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  'settings': '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 11-2.83 2.83l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 11-4 0v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 11-2.83-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 110-4h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 112.83-2.83l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 114 0v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 112.83 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 110 4h-.09a1.65 1.65 0 00-1.51 1z"/>',
  'search': '<circle cx="11" cy="11" r="7"/><path d="M21 21l-4.3-4.3"/>',
  'download': '<path d="M12 4v12M6 11l6 6 6-6"/><path d="M4 20h16"/>',
  'chevron-down': '<path d="M6 9l6 6 6-6"/>',
  'chevron-right': '<path d="M9 6l6 6-6 6"/>',
  'chevron-left': '<path d="M15 6l-6 6 6 6"/>',
  'arrow-up': '<path d="M12 19V5M6 11l6-6 6 6"/>',
  'arrow-down': '<path d="M12 5v14M6 13l6 6 6-6"/>',
  'arrow-up-right': '<path d="M7 17L17 7M8 7h9v9"/>',
  'arrow-down-right': '<path d="M7 7l10 10M17 8v9H8"/>',
  'dollar': '<path d="M12 1v22"/><path d="M17 5H9.5a3.5 3.5 0 000 7h5a3.5 3.5 0 010 7H6"/>',
  'cpu': '<rect x="6" y="6" width="12" height="12" rx="2"/><rect x="9.5" y="9.5" width="5" height="5" rx="1"/><path d="M9 2v2M15 2v2M9 20v2M15 20v2M2 9h2M2 15h2M20 9h2M20 15h2"/>',
  'zap': '<path d="M13 2L4 14h7l-1 8 9-12h-7z"/>',
  'git-branch': '<circle cx="6" cy="6" r="2.4"/><circle cx="6" cy="18" r="2.4"/><circle cx="18" cy="8" r="2.4"/><path d="M6 8.4v7.2M18 10.4c0 4-6 1.6-6 5.6"/>',
  'message-square': '<path d="M21 11.5a8.38 8.38 0 01-9 8 9 9 0 01-4-1L3 20l1.5-4.5A8.38 8.38 0 0112 3a8.5 8.5 0 019 8.5z"/>',
  'hash': '<path d="M4 9h16M4 15h16M10 3L8 21M16 3l-2 18"/>',
  'filter': '<path d="M22 3H2l8 9.46V19l4 2v-8.54L22 3z"/>',
  'more-horizontal': '<circle cx="5" cy="12" r="1.2"/><circle cx="12" cy="12" r="1.2"/><circle cx="19" cy="12" r="1.2"/>',
  'check': '<path d="M20 6L9 17l-5-5"/>',
  'x': '<path d="M18 6L6 18M6 6l12 12"/>',
  'terminal': '<path d="M4 17l6-6-6-6"/><path d="M12 19h8"/>',
  'trending-up': '<path d="M3 17l6-6 4 4 8-8"/><path d="M17 7h4v4"/>',
  'trending-down': '<path d="M3 7l6 6 4-4 8 8"/><path d="M17 17h4v-4"/>',
  'alert-triangle': '<path d="M12 3L2 20h20L12 3z"/><path d="M12 10v4M12 17v.01"/>',
  'plus': '<path d="M12 5v14M5 12h14"/>',
  'database': '<ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v6c0 1.66 3.58 3 8 3s8-1.34 8-3V5"/><path d="M4 11v6c0 1.66 3.58 3 8 3s8-1.34 8-3v-6"/>',
  'bell': '<path d="M18 8a6 6 0 00-12 0c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.7 21a2 2 0 01-3.4 0"/>',
  'calendar': '<rect x="3" y="4" width="18" height="18" rx="2"/><path d="M16 2v4M8 2v4M3 10h18"/>',
  'file-text': '<path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/><path d="M14 2v6h6M8 13h8M8 17h6"/>',
  'pencil': '<path d="M17 3a2.8 2.8 0 014 4L7.5 20.5 2 22l1.5-5.5L17 3z"/>',
  'file-plus': '<path d="M14 2H6a2 2 0 00-2 2v16a2 2 0 002 2h12a2 2 0 002-2V8z"/><path d="M14 2v6h6M12 12v6M9 15h6"/>',
  'globe': '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18"/>',
  'activity': '<path d="M3 12h4l3 8 4-16 3 8h4"/>',
  'sliders': '<path d="M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6"/>',
  'refresh': '<path d="M21 12a9 9 0 11-3-6.7L21 8"/><path d="M21 3v5h-5"/>',
  'external-link': '<path d="M15 3h6v6"/><path d="M10 14L21 3"/><path d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6"/>',
  'info': '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8v.01"/>',
  'circle': '<circle cx="12" cy="12" r="9"/>',
  'dot': '<circle cx="12" cy="12" r="4"/>',
  'layers': '<path d="M12 3l9 5-9 5-9-5 9-5z"/><path d="M3 13l9 5 9-5"/>',
  'users': '<circle cx="9" cy="8" r="3.5"/><path d="M3 20a6 6 0 0112 0"/><path d="M16 5a3.5 3.5 0 010 7M21 20a6 6 0 00-5-5.9"/>',
  'credit-card': '<rect x="2" y="5" width="20" height="14" rx="2"/><path d="M2 10h20"/>',
  'play': '<path d="M6 4l14 8-14 8z"/>',
  'key': '<circle cx="8" cy="15" r="4"/><path d="M10.8 12.2L20 3M16 7l3 3M14 9l2 2"/>',
  'copy': '<rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 01-2-2V4a2 2 0 012-2h9a2 2 0 012 2v1"/>',
  'menu': '<path d="M3 6h18M3 12h18M3 18h18"/>',
} as const

export type IconName = keyof typeof ICON_PATHS

interface IconProps {
  name: IconName
  size?: number
  stroke?: number
  color?: string
  fill?: string
  style?: CSSProperties
}

export function Icon({ name, size = 18, stroke = 1.75, color = 'currentColor', fill = 'none', style }: IconProps) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill={fill}
      stroke={color}
      strokeWidth={stroke}
      strokeLinecap="round"
      strokeLinejoin="round"
      style={{ flexShrink: 0, display: 'block', ...style }}
      dangerouslySetInnerHTML={{ __html: ICON_PATHS[name] || '' }}
    />
  )
}
