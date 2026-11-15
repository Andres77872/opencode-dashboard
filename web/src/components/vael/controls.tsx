/* Vael controls — Button, IconButton, SegmentedControl, Popover, MenuItem,
   PickerButton, Select, SearchInput. Ported from the Vael ui_kit (inline styles + CSS vars). */
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { CSSProperties, KeyboardEvent as ReactKeyboardEvent, ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { Icon, type IconName } from './icon'
import { useEscapeKey } from './overlay'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ControlSize = 'sm' | 'md'

export interface ButtonProps {
  children?: ReactNode
  variant?: ButtonVariant
  size?: ControlSize
  iconLeft?: IconName
  onClick?: () => void
  disabled?: boolean
  /** Request in flight: shows a spinner in place of the icon and blocks further clicks. */
  busy?: boolean
  title?: string
  type?: 'button' | 'submit'
  ariaLabel?: string
}

/** Push button in primary/secondary/ghost/danger variants, sm/md sizes, with optional leading icon and busy state. @category Controls */
export function Button({ children, variant = 'secondary', size = 'md', iconLeft, onClick, disabled, busy = false, title, type = 'button', ariaLabel }: ButtonProps) {
  const [hover, setHover] = useState(false)
  const pal = {
    primary: { bg: 'var(--accent)', bgHover: 'var(--accent-hover)', fg: 'var(--fg-on-accent)', fgHover: 'var(--fg-on-accent)', bd: 'transparent', bdHover: 'transparent' },
    secondary: { bg: 'var(--ink-750)', bgHover: 'var(--ink-700)', fg: 'var(--fg-primary)', fgHover: 'var(--fg-primary)', bd: 'var(--border-default)', bdHover: 'var(--border-strong)' },
    ghost: { bg: 'transparent', bgHover: 'var(--ink-750)', fg: 'var(--fg-secondary)', fgHover: 'var(--fg-primary)', bd: 'transparent', bdHover: 'transparent' },
    danger: { bg: 'transparent', bgHover: 'var(--danger-soft)', fg: 'var(--danger)', fgHover: 'var(--danger)', bd: 'var(--border-default)', bdHover: 'var(--danger)' },
  }[variant]
  const inert = disabled || busy
  const active = hover && !inert
  const h = size === 'sm' ? 28 : 32
  const icon: IconName | undefined = busy ? 'refresh' : iconLeft
  return (
    <button
      type={type}
      onClick={inert ? undefined : onClick}
      disabled={disabled}
      aria-disabled={busy || undefined}
      aria-busy={busy || undefined}
      aria-label={ariaLabel}
      title={title}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        justifyContent: 'center',
        gap: 7,
        height: h,
        padding: size === 'sm' ? '0 10px' : '0 13px',
        font: '600 13px/1 var(--font-ui)',
        color: active ? pal.fgHover : pal.fg,
        background: active ? pal.bgHover : pal.bg,
        border: `1px solid ${active ? pal.bdHover : pal.bd}`,
        borderRadius: 'var(--radius-md)',
        cursor: disabled ? 'not-allowed' : busy ? 'progress' : 'pointer',
        opacity: disabled ? 0.5 : busy ? 0.75 : 1,
        whiteSpace: 'nowrap',
        flexShrink: 0,
        transition: 'background var(--dur-fast), border-color var(--dur-fast), color var(--dur-fast)',
      }}
    >
      {icon && <Icon name={icon} size={15} style={busy ? { animation: 'vael-spin 0.9s linear infinite' } : undefined} />}
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
  /** Set when the button opens a popup, so assistive tech announces its state. */
  expanded?: boolean
}

/** Square icon-only button with hover/active states and an optional spinning state for refresh actions. @category Controls */
export function IconButton({ name, label, active, onClick, size = 32, spinning, disabled, expanded }: IconButtonProps) {
  const [h, setH] = useState(false)
  return (
    <button
      type="button"
      aria-label={label}
      aria-expanded={expanded}
      aria-haspopup={expanded === undefined ? undefined : 'dialog'}
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
        color: disabled ? 'var(--fg-faint)' : active ? 'var(--accent)' : h ? 'var(--fg-primary)' : 'var(--fg-muted)',
        background: active ? 'var(--accent-soft)' : h ? 'var(--ink-750)' : 'transparent',
        border: `1px solid ${active ? 'var(--border-accent)' : 'transparent'}`,
        borderRadius: 'var(--radius-md)',
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.55 : 1,
        transition: 'background var(--dur-fast), border-color var(--dur-fast), color var(--dur-fast)',
      }}
    >
      <Icon name={name} size={size <= 26 ? 14 : 17} style={spinning ? { animation: 'vael-spin 0.9s linear infinite' } : undefined} />
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
              // No inline box-shadow: it would outrank the global
              // :focus-visible ring and leave keyboard focus invisible.
              outline: active ? '1px solid var(--border-default)' : 'none',
              outlineOffset: -1,
              cursor: 'pointer',
              whiteSpace: 'nowrap',
              transition: 'background var(--dur-fast) var(--ease-out), color var(--dur-fast) var(--ease-out)',
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
  /** A render prop receives a close callback, for panels that stay open on click. */
  children: ReactNode | ((close: () => void) => ReactNode)
  align?: 'left' | 'right'
  width?: number
  closeOnClick?: boolean
  /**
   * Render the panel into document.body instead of next to the trigger. An
   * absolutely positioned panel is clipped by any scroll container it sits in
   * (a DataTable's horizontal scroller, for one), which silently truncates the
   * list. Opt in wherever the trigger can live inside such a container.
   */
  portal?: boolean
  /** Notified when the panel opens or closes, for focus handling. */
  onOpenChange?: (open: boolean) => void
  /** Names the panel and exposes it as a dialog; use for panels that are more than a menu. */
  label?: string
  /** Drop the panel's own padding, for content that draws its own header and dividers. */
  flush?: boolean
  /** Fill the container's width instead of hugging the trigger, for a picker used as a form field. */
  block?: boolean
}

/** Anchored dropdown surface — render-prop trigger toggles a floating panel; closes on outside click. @category Controls */
export function Popover({ trigger, children, align = 'left', width, closeOnClick = true, portal, onOpenChange, label, flush = false, block = false }: PopoverProps) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)
  const [anchor, setAnchor] = useState<CSSProperties | null>(null)

  const setOpenState = useCallback((next: boolean) => setOpen(next), [])

  // Notified from an effect rather than from the setter: React may invoke a
  // state updater more than once, which would double-fire the callback.
  const notifiedRef = useRef(open)
  useEffect(() => {
    if (notifiedRef.current === open) return
    notifiedRef.current = open
    onOpenChange?.(open)
  }, [open, onOpenChange])

  // The listener exists only while the panel is open: a closed popover per
  // table row should not each hold a document-level handler.
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      const target = e.target as Node
      if (ref.current?.contains(target) || panelRef.current?.contains(target)) return
      // A modal opened from this panel (a confirmation, say) sits outside it
      // in the DOM but belongs to it: answering it must not close the panel.
      const modal = target instanceof Element ? target.closest('[data-vael-modal]') : null
      if (modal && !modal.contains(ref.current)) return
      setOpenState(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open, setOpenState])

  // A dialog-style panel takes focus when it opens so the keyboard lands in it
  // (a portalled panel sits at the end of the document, far from its trigger).
  // Deferred a frame: a portalled panel is not focusable until it is positioned.
  useEffect(() => {
    if (!open || !label) return
    const frame = requestAnimationFrame(() => panelRef.current?.focus())
    return () => cancelAnimationFrame(frame)
  }, [open, label])

  useEscapeKey(open, () => {
    setOpenState(false)
    // Hand focus back to whatever opened the panel.
    ref.current?.querySelector<HTMLElement>('button, [tabindex]')?.focus()
  })

  // Track the trigger's viewport box while open. Scroll and resize both move it,
  // and a fixed panel does not follow its anchor on its own.
  //
  // A closed panel is unmounted, so a stale anchor is never rendered and there
  // is no need to clear it here. On reopen this effect repositions before the
  // browser paints, so the panel is never shown at its previous location.
  useLayoutEffect(() => {
    if (!portal || !open) return
    const reposition = () => {
      const trigger = ref.current
      if (!trigger) return
      const box = trigger.getBoundingClientRect()
      const panelHeight = panelRef.current?.offsetHeight ?? 0
      const below = window.innerHeight - box.bottom
      // Flip above only when the panel genuinely does not fit below.
      const flip = panelHeight > 0 && below < panelHeight + 12 && box.top > below
      // Keep the panel on screen on both sides: measure it once mounted, and
      // fall back to the requested width (capped to the viewport) before that.
      const panelWidth = panelRef.current?.offsetWidth || Math.min(width ?? 240, window.innerWidth - 16)
      const horizontal: CSSProperties = align === 'right'
        ? { right: Math.max(8, Math.min(window.innerWidth - box.right, window.innerWidth - panelWidth - 8)) }
        : { left: Math.max(8, Math.min(box.left, window.innerWidth - panelWidth - 8)) }
      setAnchor({
        position: 'fixed',
        ...horizontal,
        ...(flip ? { bottom: window.innerHeight - box.top + 6 } : { top: box.bottom + 6 }),
        maxHeight: Math.max(160, (flip ? box.top : below) - 16),
      })
    }
    reposition()
    window.addEventListener('scroll', reposition, true)
    window.addEventListener('resize', reposition)
    return () => {
      window.removeEventListener('scroll', reposition, true)
      window.removeEventListener('resize', reposition)
    }
  }, [align, open, portal, width])

  const alignStyle: CSSProperties = align === 'right' ? { right: 0 } : { left: 0 }
  const panelStyle: CSSProperties = {
    ...(portal
      ? { position: 'fixed', visibility: anchor ? 'visible' : 'hidden', overflowY: 'auto', ...anchor }
      : { position: 'absolute', top: 'calc(100% + 6px)', maxHeight: 'calc(100vh - 140px)', overflowY: 'auto', ...alignStyle }),
    zIndex: portal ? 'var(--z-popover-portal)' : 'var(--z-popover)',
    // Never wider than the viewport: a fixed pixel width ran off-screen on phones.
    width,
    maxWidth: 'calc(100vw - 16px)',
    background: 'var(--ink-700)',
    border: '1px solid var(--border-strong)',
    borderRadius: 'var(--radius-lg)',
    boxShadow: 'var(--shadow-lg)',
    padding: flush ? 0 : 5,
  }
  const close = () => setOpenState(false)
  const panel = open ? (
    <div ref={panelRef} role={label ? 'dialog' : undefined} aria-label={label} tabIndex={label ? -1 : undefined} style={{ ...panelStyle, outline: 'none' }} onClick={closeOnClick ? close : undefined}>
      {typeof children === 'function' ? children(close) : children}
    </div>
  ) : null

  return (
    <div ref={ref} style={{ position: 'relative', display: block ? 'grid' : 'inline-flex', minWidth: block ? 0 : undefined }}>
      {trigger(open, () => setOpenState(!open))}
      {portal && panel ? createPortal(panel, document.body) : portal ? null : panel}
    </div>
  )
}

export interface MenuItemProps {
  children: ReactNode
  selected?: boolean
  color?: string | null
  disabled?: boolean
  onClick?: () => void
}

/** Row inside a Popover/Select menu — optional color swatch, selected check. @category Controls */
export function MenuItem({ children, selected, color, disabled, onClick }: MenuItemProps) {
  const [hover, setHover] = useState(false)
  return (
    <button
      type="button"
      role="menuitemradio"
      aria-checked={Boolean(selected)}
      disabled={disabled}
      onClick={onClick}
      onMouseEnter={() => setHover(true)}
      onMouseLeave={() => setHover(false)}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        width: '100%',
        padding: '8px 9px',
        border: 'none',
        borderRadius: 'var(--radius-sm)',
        background: selected ? 'var(--accent-soft)' : hover && !disabled ? 'var(--ink-650)' : 'transparent',
        color: disabled ? 'var(--fg-faint)' : selected ? 'var(--blue-300)' : 'var(--fg-secondary)',
        font: '500 13px/1 var(--font-ui)',
        textAlign: 'left',
        cursor: disabled ? 'not-allowed' : 'pointer',
        whiteSpace: 'nowrap',
      }}
    >
      {color && <span style={{ width: 8, height: 8, borderRadius: 2, background: color }} />}
      <span style={{ flex: 1 }}>{children}</span>
      {selected && <Icon name="check" size={14} color="var(--accent)" />}
    </button>
  )
}

export interface PickerButtonProps {
  children: ReactNode
  open: boolean
  onClick: () => void
  icon?: IconName
  iconColor?: string
  /** Muted prefix shown before the value ("Sort", "Model"). */
  label?: ReactNode
  disabled?: boolean
  /** The kind of popup this opens, announced to assistive tech. */
  popup?: 'menu' | 'dialog' | 'listbox'
  ariaLabel?: string
  onKeyDown?: (event: ReactKeyboardEvent<HTMLButtonElement>) => void
  style?: CSSProperties
}

/** The trigger every dropdown shares — icon, optional label prefix, current value, chevron. The value truncates rather than widening the bar. @category Controls */
export function PickerButton({ children, open, onClick, icon, iconColor = 'var(--fg-muted)', label, disabled, popup = 'menu', ariaLabel, onKeyDown, style }: PickerButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      onKeyDown={onKeyDown}
      disabled={disabled}
      aria-haspopup={popup}
      aria-expanded={open}
      aria-label={ariaLabel}
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 7,
        height: 32,
        minWidth: 0,
        maxWidth: '100%',
        padding: '0 10px',
        background: open ? 'var(--ink-700)' : 'var(--ink-750)',
        border: `1px solid ${open ? 'var(--border-accent)' : 'var(--border-default)'}`,
        borderRadius: 'var(--radius-md)',
        cursor: disabled ? 'default' : 'pointer',
        font: '500 13px/1 var(--font-ui)',
        color: disabled ? 'var(--fg-muted)' : 'var(--fg-primary)',
        whiteSpace: 'nowrap',
        ...style,
      }}
    >
      {icon && <Icon name={icon} size={15} color={iconColor} style={{ flexShrink: 0 }} />}
      {label && <span style={{ color: 'var(--fg-muted)', flexShrink: 0 }}>{label}</span>}
      <span style={{ flex: 1, minWidth: 0, textAlign: 'left', overflow: 'hidden', textOverflow: 'ellipsis' }}>{children}</span>
      <Icon name="chevron-down" size={14} color="var(--fg-muted)" style={{ flexShrink: 0, transform: open ? 'rotate(180deg)' : 'none', transition: 'transform var(--dur-fast)' }} />
    </button>
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
        <PickerButton open={open} onClick={toggle} icon={icon} label={label}>
          {curLabel}
        </PickerButton>
      )}
    >
      {options.map((o) => {
        const val = (typeof o === 'string' ? o : o.value) as V
        const lbl = typeof o === 'string' ? o : o.label
        const optColor = typeof o === 'object' ? o.color : null
        const disabled = typeof o === 'object' ? o.disabled : false
        return (
          <MenuItem key={val} selected={val === value} color={optColor} disabled={disabled} onClick={() => onChange && onChange(val)}>
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
    // Sized by width, not flex-basis: a basis is ignored outside a flex row
    // and makes a wrapping row break early. It still shrinks in a tight one.
    <div style={{ position: 'relative', flex: '0 1 auto', width, maxWidth: '100%', minWidth: 'min(180px, 100%)' }}>
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
        spellCheck={false}
        autoComplete="off"
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
