/* Icon — inline Lucide-style paths (ported from the Vael ui_kit), so React
   re-renders never wipe them and no icon font/runtime dependency is needed.
   The path table lives in ./icon-paths so this file exports only the
   component and stays a valid Fast Refresh boundary. */
import type { CSSProperties } from 'react'

import { ICON_PATHS, type IconName } from './icon-paths'

// Part of Icon's public surface, so it stays importable from here. Type-only
// exports are erased at build time and do not affect the Fast Refresh boundary.
export type { IconName }

interface IconProps {
  name: IconName
  size?: number
  stroke?: number
  color?: string
  fill?: string
  style?: CSSProperties
}

/** Inline stroke icon from the Vael Lucide-style path set (see IconName for the full set). @category Icons */
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
