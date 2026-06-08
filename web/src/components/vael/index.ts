/* Vael component library — barrel export.
   Import design-system components from '@/components/vael' (relative paths). */
export { Icon, ICON_PATHS, type IconName } from './icon'
export { VENDORS, vendorMeta, type VendorMeta } from './vendors'

export {
  DeltaChip,
  Badge,
  VendorChip,
  Avatar,
  Legend,
  SourceStack,
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

export { Card, StatCard, SectionTitle, type CardProps, type StatCardProps } from './card'

export {
  Button,
  IconButton,
  SegmentedControl,
  Popover,
  MenuItem,
  Select,
  type ButtonVariant,
  type ControlSize,
  type SegmentedOption,
  type SelectOption,
} from './controls'

export { DataTable, type Column, type SortSpec, type SortDir } from './data-table'

export {
  AreaChart,
  StackedBars,
  Donut,
  BudgetRing,
  Heatmap,
  useWidth,
  niceMax,
  type AreaSeries,
  type StackedBarDay,
  type StackedBarKey,
  type DonutSegment,
  type HeatmapCell,
} from './charts'

export { Drawer, type DrawerProps } from './drawer'
export { Tabs, type TabItem } from './tabs'
export { Tooltip, type TooltipSide } from './tooltip'
export { Skeleton, EmptyState, ErrorState, Notice, type NoticeTone } from './feedback'
