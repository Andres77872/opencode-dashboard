import type { ReactNode } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'

interface EmptyStateCardProps {
  title: string
  eyebrow?: string
  children?: ReactNode
  className?: string
}

/** Shared "no data yet" card used across stats views. */
export function EmptyStateCard({ title, eyebrow = 'Empty state', children, className }: EmptyStateCardProps) {
  return (
    <Card className={className}>
      <CardHeader>
        <CardDescription>{eyebrow}</CardDescription>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      {children ? (
        <CardContent className="space-y-3 text-sm text-muted-foreground">{children}</CardContent>
      ) : null}
    </Card>
  )
}
