/* Vael component library — barrel export.
   Import design-system components from '@/components/vael' (relative paths). */
export { Icon } from './icon'
export { ICON_PATHS, type IconName } from './icon-paths'
export { VENDORS, vendorMeta, type VendorMeta } from './vendors'

export {
  DeltaChip,
  Badge,
  VendorChip,
  Avatar,
  Legend,
  SourceStack,
  ShareBar,
  BarRow,
  Sparkline,
  RSpark,
  type DeltaDir,
  type DeltaTone,
  type DeltaChipProps,
  type BadgeTone,
  type LegendItem,
  type SourceStackItem,
} from './atoms'

export { Card, StatCard, StatGrid, SectionTitle, KeyValueRow, type CardProps, type StatCardProps } from './card'

export {
  Button,
  IconButton,
  SegmentedControl,
  Popover,
  PickerButton,
  MenuItem,
  Select,
  SearchInput,
  type ButtonVariant,
  type ControlSize,
  type SegmentedOption,
  type SelectOption,
  type SearchInputProps,
} from './controls'

export { DataTable, type Column, type SortSpec, type SortDir } from './data-table'

export {
  AreaChart,
  StackedBars,
  Donut,
  BudgetRing,
  Heatmap,
  type AreaSeries,
  type StackedBarDay,
  type StackedBarKey,
  type DonutSegment,
  type HeatmapCell,
} from './charts'

export { useWidth, niceMax } from './chart-utils'

export { CompositionBar, type CompositionItem } from './composition-bar'
export { Pagination, type PaginationProps } from './pagination'
export { Field, TextField, Switch, type FieldProps, type TextFieldProps, type SwitchProps } from './forms'
export { ConfirmDialog, type ConfirmDialogProps } from './dialog'
export { useEscapeKey, useFocusTrap } from './overlay'

export { Drawer, type DrawerProps } from './drawer'
export { Tabs, type TabItem } from './tabs'
export { Tooltip, type TooltipSide } from './tooltip'
export { Skeleton, ViewSkeleton, EmptyState, ErrorState, Notice, type NoticeTone } from './feedback'
