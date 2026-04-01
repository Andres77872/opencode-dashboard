import type { DailyPeriod } from '../../types/api'
import { SegmentedControl } from './segmented-control'

interface PeriodToggleProps {
  value: DailyPeriod
  onChange: (value: DailyPeriod) => void
  disabled?: boolean
}

const options: Array<{ label: string; value: DailyPeriod }> = [
  { label: '7 days', value: '7d' },
  { label: '30 days', value: '30d' },
]

export function PeriodToggle({ value, onChange, disabled = false }: PeriodToggleProps) {
  return (
    <SegmentedControl ariaLabel="Daily period" disabled={disabled} onChange={onChange} options={options} value={value} />
  )
}
