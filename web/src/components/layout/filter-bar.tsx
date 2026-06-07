import { useLocation } from 'react-router-dom'
import { PeriodPicker } from '../daily/period-picker'
import { usePeriodControls } from '../../lib/use-period-controls'

/**
 * Global time-range filter, defined once and rendered in the layout shell beneath
 * the header. Because the period is fully URL-driven, changing it here updates every
 * view through their existing usePeriodResource(cacheKey) without any prop drilling.
 * Hidden on /config, which has no period.
 */
export function FilterBar() {
  const location = useLocation()
  // Reset URL-driven list pagination when the range changes (only sessions uses
  // ?page=; this is a no-op for views that don't paginate via the URL).
  const { pickerProps } = usePeriodControls({
    mutateUrl: (params) => {
      if (params.has('page')) {
        params.set('page', '1')
      }
    },
  })

  if (location.pathname.startsWith('/config')) {
    return null
  }

  return (
    <div className="border-b border-border/50 bg-background/70 backdrop-blur-xl">
      <div className="mx-auto flex h-12 w-full max-w-7xl items-center justify-between gap-3 px-6 xl:px-8">
        <span className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
          Time range
        </span>
        <PeriodPicker {...pickerProps} />
      </div>
    </div>
  )
}
