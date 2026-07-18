/* Vael controls — Button, IconButton, SegmentedControl, Popover, MenuItem,
   Select. Ported from the Vael ui_kit (inline styles + CSS vars). */
import { useEffect, useRef, useState } from 'react'
import type { CSSProperties, ReactNode } from 'react'
import { Icon, type IconName } from './icon'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost'
export type ControlSize = 'sm' | 'md'

export interface ButtonProps {
  children?: ReactNode
  variant?: ButtonVariant
  size?: ControlSize
  iconLeft?: IconName
  onClick?: () => void
  disabled?: boolean
  title?: string
  type?: 'button' | 'submit'
}

/** Push button in primary/secondary/ghost variants, sm/md sizes, with optional leading icon. @category Controls */
export function Button({ children, variant = 'secondary', size = 'md', iconLeft, onClick, disabled, title, type = 'button' }: ButtonProps) {
  const [hover, setHover] = useState(false)
  const pal = {
    primary: { bg: 'var(--accent)', bgHover: 'var(--accent-hover)', fg: 'var(--fg-on-accent)', bd: 'transparent', bdHover: 'transparent' },
    secondary: { bg: 'var(--ink-750)', bgHover: 'var(--ink-700)', fg: 'var(--fg-primary)', bd: 'var(--border-default)', bdHover: 'var(--border-strong)' },
    ghost: { bg: 'transparent', bgHover: 'var(--ink-750)', fg: 'var(--fg-secondary)', bd: 'transparent', bdHover: 'transparent' },
  }[variant]
  const active = hover && !disabled
  const h = size === 'sm' ? 28 : 32
  return (
    <button
      type={type}
      onClick={onClick}
      disabled={disabled}
      title={title}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 7,
        height: h,
        padding: size === 'sm' ? '0 10px' : '0 13px',
        font: '600 13px/1 var(--font-ui)',
        color: active && variant === 'ghost' ? 'var(--fg-primary)' : pal.fg,
        background: active ? pal.bgHover : pal.bg,
        border: `1px solid ${active ? pal.bdHover : pal.bd}`,
        borderRadius: 'var(--radius-md)',
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.5 : 1,
        whiteSpace: 'nowrap',
        flexShrink: 0,
        transition: 'background var(--dur-fast), border-color var(--dur-fast), color var(--dur-fast)',
      }}
    >
      {iconLeft && <Icon name={iconLeft} size={15} />}
      {children}
    </button>
  )
}

export interface IconButtonProps {
  name: IconName
  label: string
  active?: boolean
  onClick?: () => void
  size?: number
  spinning?: boolean
  disabled?: boolean
}

/** Square icon-only button with hover/active states and an optional spinning state for refresh actions. @category Controls */
export function IconButton({ name, label, active, onClick, size = 32, spinning, disabled }: IconButtonProps) {
  const [h, setH] = useState(false)
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={disabled ? undefined : onClick}
      disabled={disabled}
      onMouseEnter={() => setH(true)}
      onMouseLeave={() => setH(false)}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        width: size,
        height: size,
        color: disabled ? 'var(--fg-subtle)' : active ? 'var(--accent)' : h ? 'var(--fg-primary)' : 'var(--fg-muted)',
        background: active ? 'var(--accent-soft)' : h ? 'var(--ink-750)' : 'transparent',
        border: `1px solid ${active ? 'var(--border-accent)' : 'transparent'}`,
        borderRadius: 'var(--radius-md)',
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.55 : 1,
        transition: 'all var(--dur-fast)',
      }}
    >
      <Icon name={name} size={17} style={spinning ? { animation: 'vael-spin 0.9s linear infinite' } : undefined} />
    </button>
  )
}

export type SegmentedOption<V extends string> = V | { value: V; label: ReactNode }

export interface SegmentedControlProps<V extends string> {
  options: SegmentedOption<V>[]
  value: V
  onChange?: (value: V) => void
  size?: ControlSize
  /** Accessible name for the exclusive button group. */
  ariaLabel?: string
}

/** Inline segmented switch for small exclusive option sets (metric, granularity). @category Controls */
export function SegmentedControl<V extends string>({ options, value, onChange, size = 'md', ariaLabel }: SegmentedControlProps<V>) {
  const h = size === 'sm' ? 28 : 32
  return (
    <div
      role="group"
      aria-label={ariaLabel}
      style={{
        display: 'inline-flex',
        padding: 3,
        gap: 2,
        background: 'var(--ink-850)',
        border: '1px solid var(--border-default)',
        borderRadius: 'var(--radius-md)',
        height: h,
      }}
    >
      {options.map((o) => {
        const val = (typeof o === 'string' ? o : o.value) as V
        const lbl = typeof o === 'string' ? o : o.label
        const active = val === value
        return (
          <button
            key={val}
            type="button"
            aria-pressed={active}
            onClick={() => onChange && onChange(val)}
            style={{
              display: 'inline-flex',
              alignItems: 'center',
              gap: 6,
              padding: '0 11px',
              height: h - 8,
              border: 'none',
              borderRadius: 'var(--radius-sm)',
              font: `${active ? 600 : 500} 12px/1 var(--font-ui)`,
              color: active ? 'var(--fg-primary)' : 'var(--fg-muted)',
              background: active ? 'var(--ink-700)' : 'transparent',
              boxShadow: active ? 'var(--shadow-sm)' : 'none',
              cursor: 'pointer',
              whiteSpace: 'nowrap',
              transition: 'all var(--dur-fast) var(--ease-out)',
            }}
          >
            {lbl}
          </button>
        )
      })}
    </div>
  )
}

export interface PopoverProps {
  trigger: (open: boolean, toggle: () => void) => ReactNode
  children: ReactNode
  align?: 'left' | 'right'
  width?: number
  closeOnClick?: boolean
}

/** Anchored dropdown surface — render-prop trigger toggles a floating panel; closes on outside click. @category Controls */
export function Popover({ trigger, children, align = 'left', width, closeOnClick = true }: PopoverProps) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const h = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', h)
    return () => document.removeEventListener('mousedown', h)
  }, [])
  const alignStyle: CSSProperties = align === 'right' ? { right: 0 } : { left: 0 }
  return (
    <div ref={ref} style={{ position: 'relative', display: 'inline-flex' }}>
      {trigger(open, () => setOpen((v) => !v))}
      {open && (
        <div
          style={{
            position: 'absolute',
            top: 'calc(100% + 6px)',
            ...alignStyle,
            zIndex: 60,
            width,
            background: 'var(--ink-700)',
            border: '1px solid var(--border-strong)',
            borderRadius: 'var(--radius-lg)',
            boxShadow: 'var(--shadow-lg)',
            padding: 5,
          }}
          onClick={closeOnClick ? () => setOpen(false) : undefined}
        >
          {children}
        </div>
      )}
    </div>
  )
}

export interface MenuItemProps {
  children: ReactNode
  selected?: boolean
  color?: string | null
  onClick?: () => void
}

/** Row inside a Popover/Select menu — optional color swatch, selected check. @category Controls */
export function MenuItem({ children, selected, color, onClick }: MenuItemProps) {
  return (
    <div
      onClick={onClick}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        padding: '8px 9px',
        borderRadius: 'var(--radius-sm)',
        background: selected ? 'var(--accent-soft)' : 'transparent',
        color: selected ? 'var(--blue-300)' : 'var(--fg-secondary)',
        font: '500 13px/1 var(--font-ui)',
        cursor: 'pointer',
        whiteSpace: 'nowrap',
      }}
      onMouseEnter={(e) => {
        if (!selected) e.currentTarget.style.background = 'var(--ink-650)'
      }}
      onMouseLeave={(e) => {
        if (!selected) e.currentTarget.style.background = 'transparent'
      }}
    >
      {color && <span style={{ width: 8, height: 8, borderRadius: 2, background: color }} />}
      <span style={{ flex: 1 }}>{children}</span>
      {selected && <Icon name="check" size={14} color="var(--accent)" />}
    </div>
  )
}

export interface SelectOptionObject<V extends string> {
  value: V
  label: ReactNode
  color?: string
  disabled?: boolean
}
export type SelectOption<V extends string> = V | SelectOptionObject<V>

export interface SelectProps<V extends string> {
  label?: ReactNode
  icon?: IconName
  value: V
  options: SelectOption<V>[]
  onChange?: (value: V) => void
  width?: number
}

/** Dropdown select built on Popover + MenuItem, with optional leading icon and label prefix. @category Controls */
export function Select<V extends string>({ label, icon, value, options, onChange, width }: SelectProps<V>) {
  const current = options.find((o) => (typeof o === 'string' ? o : o.value) === value)
  const curLabel = current ? (typeof current === 'string' ? current : current.label) : value
  return (
    <Popover
      width={width || 200}
      trigger={(open, toggle) => (
        <button
          type="button"
          onClick={toggle}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 7,
            height: 32,
            padding: '0 10px',
            background: open ? 'var(--ink-700)' : 'var(--ink-750)',
            border: `1px solid ${open ? 'var(--border-accent)' : 'var(--border-default)'}`,
            borderRadius: 'var(--radius-md)',
            cursor: 'pointer',
            font: '500 13px/1 var(--font-ui)',
            color: 'var(--fg-primary)',
            whiteSpace: 'nowrap',
          }}
        >
          {icon && <Icon name={icon} size={15} color="var(--fg-muted)" />}
          {label && <span style={{ color: 'var(--fg-muted)' }}>{label}</span>}
          <span>{curLabel}</span>
          <Icon name="chevron-down" size={14} color="var(--fg-muted)" style={{ transform: open ? 'rotate(180deg)' : 'none', transition: 'transform var(--dur-fast)' }} />
        </button>
      )}
    >
      {options.map((o) => {
        const val = (typeof o === 'string' ? o : o.value) as V
        const lbl = typeof o === 'string' ? o : o.label
        const optColor = typeof o === 'object' ? o.color : null
        const disabled = typeof o === 'object' ? o.disabled : false
        return (
          <MenuItem key={val} selected={val === value} color={optColor} onClick={disabled ? undefined : () => onChange && onChange(val)}>
            {lbl}
          </MenuItem>
        )
      })}
    </Popover>
  )
}

export interface SearchInputProps {
  value: string
  onChange: (value: string) => void
  placeholder?: string
  /** Accessible name. Defaults to the placeholder. */
  label?: string
  width?: number | string
}

/**
 * Text filter for a table. Client-side by convention: it narrows rows the view
 * already holds, which is why it carries no debounce (compare the Sessions
 * search box, which hits the paginated API and therefore does).
 *
 * @category Controls
 */
export function SearchInput({ value, onChange, placeholder = 'Filter…', label, width = 260 }: SearchInputProps) {
  const [focused, setFocused] = useState(false)
  return (
    <div style={{ position: 'relative', flex: `0 1 ${typeof width === 'number' ? `${width}px` : width}`, minWidth: 180 }}>
      <span
        aria-hidden
        style={{ position: 'absolute', left: 12, top: '50%', transform: 'translateY(-50%)', display: 'inline-flex', color: focused ? 'var(--fg-muted)' : 'var(--fg-faint)', pointerEvents: 'none', transition: 'color var(--dur-fast)' }}
      >
        <Icon name="search" size={15} />
      </span>
      <input
        type="text"
        aria-label={label ?? placeholder}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
        style={{
          width: '100%',
          height: 36,
          padding: value ? '0 32px 0 34px' : '0 12px 0 34px',
          background: 'var(--ink-800)',
          border: `1px solid ${focused ? 'var(--border-accent)' : 'var(--border-default)'}`,
          borderRadius: 'var(--radius-md)',
          color: 'var(--fg-primary)',
          font: '400 13px/1 var(--font-ui)',
          outline: 'none',
          boxShadow: focused ? '0 0 0 3px var(--accent-soft)' : 'none',
          transition: 'border-color var(--dur-fast), box-shadow var(--dur-fast)',
        }}
      />
      {value && (
        <button
          type="button"
          aria-label="Clear filter"
          onClick={() => onChange('')}
          style={{
            position: 'absolute',
            right: 8,
            top: '50%',
            transform: 'translateY(-50%)',
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            width: 20,
            height: 20,
            background: 'transparent',
            border: 'none',
            borderRadius: 'var(--radius-sm)',
            color: 'var(--fg-faint)',
            cursor: 'pointer',
          }}
        >
          <Icon name="x" size={13} />
        </button>
      )}
    </div>
  )
}
