import type { ReactNode } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'

interface ConfigStateCardProps {
  description: ReactNode
  title: ReactNode
  children: ReactNode
  actions?: ReactNode
}

export function ConfigStateCard({ description, title, children, actions }: ConfigStateCardProps) {
  return (
    <Card>
      <CardHeader>
        <CardDescription>{description}</CardDescription>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3 text-sm text-muted-foreground">
        {children}
        {actions ? <div className="flex flex-wrap justify-end gap-1.5">{actions}</div> : null}
      </CardContent>
    </Card>
  )
}
