import type { DailyPeriod } from '../../types/api'
import { SegmentedControl } from './segmented-control'

interface PeriodToggleProps {
  value: DailyPeriod
  onChange: (value: DailyPeriod) => void
  disabled?: boolean
}

const options: Array<{ label: string; value: DailyPeriod }> = [
  { label: '1d', value: '1d' },
  { label: '7d', value: '7d' },
  { label: '30d', value: '30d' },
  { label: '1y', value: '1y' },
  { label: 'All', value: 'all' },
]

export function PeriodToggle({ value, onChange, disabled = false }: PeriodToggleProps) {
  return (
    <SegmentedControl ariaLabel="Daily period" disabled={disabled} onChange={onChange} options={options} value={value} />
  )
}
