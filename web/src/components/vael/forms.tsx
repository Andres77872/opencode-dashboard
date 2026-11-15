/* Vael form primitives — Field, TextField, SelectField, Switch. Inputs take
   their look from the .vael-input rule in index.css (focus, invalid and
   disabled states need pseudo-classes inline styles cannot express). */
import { useId } from 'react'
import type { ChangeEvent, CSSProperties, ReactNode } from 'react'

export interface FieldProps {
  label: ReactNode
  /** Associates the label with a control rendered by the caller. */
  htmlFor?: string
  hint?: ReactNode
  error?: ReactNode
  children: ReactNode
  style?: CSSProperties
}

/** Label + control + hint/error wrapper that keeps form rows aligned. @category Controls */
export function Field({ label, htmlFor, hint, error, children, style }: FieldProps) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 0, ...style }}>
      <label htmlFor={htmlFor} style={{ font: '600 12px/1.2 var(--font-ui)', color: 'var(--fg-secondary)' }}>
        {label}
      </label>
      {children}
      {error ? (
        <span role="alert" style={{ font: '400 12px/1.4 var(--font-ui)', color: 'var(--danger)' }}>{error}</span>
      ) : hint ? (
        <span style={{ font: '400 12px/1.4 var(--font-ui)', color: 'var(--fg-muted)' }}>{hint}</span>
      ) : null}
    </div>
  )
}

export interface TextFieldProps {
  label: ReactNode
  value: string
  onChange: (value: string) => void
  type?: 'text' | 'password' | 'url' | 'number'
  placeholder?: string
  hint?: ReactNode
  error?: ReactNode
  disabled?: boolean
  required?: boolean
  /** Monospace value, for ids, URLs and keys. */
  mono?: boolean
  autoComplete?: string
  inputMode?: 'text' | 'numeric' | 'url'
  min?: number
  style?: CSSProperties
}

/** Labelled single-line input with hint and inline error. @category Controls */
export function TextField({ label, value, onChange, type = 'text', placeholder, hint, error, disabled, required, mono, autoComplete = 'off', inputMode, min, style }: TextFieldProps) {
  const id = useId()
  const describedBy = error || hint ? `${id}-note` : undefined
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 6, minWidth: 0, ...style }}>
      <label htmlFor={id} style={{ font: '600 12px/1.2 var(--font-ui)', color: 'var(--fg-secondary)' }}>
        {label}
        {required && <span aria-hidden="true" style={{ color: 'var(--fg-muted)', marginLeft: 4 }}>*</span>}
      </label>
      <input
        id={id}
        className="vael-input"
        type={type}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        required={required}
        autoComplete={autoComplete}
        spellCheck={false}
        inputMode={inputMode}
        min={min}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy}
        onChange={(event: ChangeEvent<HTMLInputElement>) => onChange(event.target.value)}
        style={mono ? { fontFamily: 'var(--font-mono)', fontSize: 12 } : undefined}
      />
      {error ? (
        <span id={describedBy} role="alert" style={{ font: '400 12px/1.4 var(--font-ui)', color: 'var(--danger)' }}>{error}</span>
      ) : hint ? (
        <span id={describedBy} style={{ font: '400 12px/1.4 var(--font-ui)', color: 'var(--fg-muted)' }}>{hint}</span>
      ) : null}
    </div>
  )
}

export interface SwitchProps {
  checked: boolean
  onChange: (checked: boolean) => void
  /** Accessible name; the visible label lives next to the switch. */
  label: string
  disabled?: boolean
  busy?: boolean
}

/** On/off toggle with switch semantics, for a setting that applies immediately. @category Controls */
export function Switch({ checked, onChange, label, disabled, busy }: SwitchProps) {
  const inert = disabled || busy
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      aria-busy={busy || undefined}
      disabled={disabled}
      onClick={inert ? undefined : () => onChange(!checked)}
      style={{
        position: 'relative',
        flexShrink: 0,
        width: 38,
        height: 22,
        padding: 0,
        borderRadius: 'var(--radius-pill)',
        border: `1px solid ${checked ? 'transparent' : 'var(--border-strong)'}`,
        background: checked ? 'var(--accent)' : 'var(--ink-700)',
        cursor: disabled ? 'not-allowed' : busy ? 'progress' : 'pointer',
        opacity: disabled ? 0.5 : busy ? 0.7 : 1,
        transition: 'background var(--dur-base) var(--ease-out), border-color var(--dur-base) var(--ease-out)',
      }}
    >
      <span
        aria-hidden="true"
        style={{
          position: 'absolute',
          top: 2,
          left: checked ? 18 : 2,
          width: 16,
          height: 16,
          borderRadius: '50%',
          background: checked ? 'var(--fg-on-accent)' : 'var(--fg-secondary)',
          transition: 'left var(--dur-base) var(--ease-out), background var(--dur-base) var(--ease-out)',
        }}
      />
    </button>
  )
}
