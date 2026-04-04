import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { CopyButton } from './copy-button'

interface ConfigRawJsonPanelProps {
  content?: string
  copiedId: string | null
  onCopy: (copyId: string, value: string) => void
}

export function ConfigRawJsonPanel({ content, copiedId, onCopy }: ConfigRawJsonPanelProps) {
  return (
    <Card className="border-border/70 bg-linear-to-b from-card to-panel">
      <CardHeader className="gap-3 border-b border-border/60">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div className="space-y-1">
            <CardDescription>Power-user view</CardDescription>
            <CardTitle className="text-xl">Raw redacted JSON snapshot</CardTitle>
          </div>

          {content ? <CopyButton copyId="raw-tab-copy" copiedId={copiedId} label="Copy JSON" value={content} onCopy={onCopy} /> : null}
        </div>
      </CardHeader>
      <CardContent className="space-y-3 pt-4">
        <p className="text-sm text-muted-foreground">
          Keep this for exact payload verification. The focused section tabs are better for scanning, but this preserves the literal snapshot the browser received.
        </p>
        <div className="max-h-[40rem] overflow-auto rounded-xl border border-border/70 bg-background/45 p-3 font-mono text-xs leading-6 text-foreground">
          <pre className="whitespace-pre-wrap break-words">{content}</pre>
        </div>
      </CardContent>
    </Card>
  )
}
