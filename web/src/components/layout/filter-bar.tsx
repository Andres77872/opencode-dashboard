/* Vael filter bar — global time-range + data-source pickers. Sticky-fixed flex
   item below the TopBar. Period is hidden on /config (no period there). */
import { useLocation } from 'react-router-dom'
import { PeriodPicker } from './period-picker'
import { SourcePicker } from '../source/source-picker'
import { usePeriodControls } from '../../lib/use-period-controls'

export function FilterBar() {
  const isConfig = useLocation().pathname.startsWith('/config')
  // Reset URL-driven pagination when the range changes (no-op for views without ?page=).
  const { pickerProps } = usePeriodControls({
    mutateUrl: (params) => {
      if (params.has('page')) params.set('page', '1')
    },
  })

  return (
    <div
      style={{
        height: 'var(--filterbar-height)',
        flexShrink: 0,
        display: 'flex',
        alignItems: 'center',
        gap: 8,
        padding: '0 24px',
        borderBottom: '1px solid var(--border-default)',
        background: 'var(--ink-850)',
      }}
    >
      {!isConfig && <PeriodPicker {...pickerProps} />}
      <SourcePicker />
    </div>
  )
}
