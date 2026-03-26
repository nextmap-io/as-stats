import { AlertTriangle, RefreshCw } from "lucide-react"
import { Card, CardContent } from "./card"

interface ErrorDisplayProps {
  error: Error
  onRetry?: () => void
  title?: string
}

export function ErrorDisplay({ error, onRetry, title = "Something went wrong" }: ErrorDisplayProps) {
  return (
    <Card className="border-destructive/30 bg-destructive/5 animate-fade-in">
      <CardContent className="p-6">
        <div className="flex items-start gap-3">
          <AlertTriangle className="h-5 w-5 text-destructive shrink-0 mt-0.5" />
          <div className="space-y-1 min-w-0">
            <p className="text-sm font-medium text-foreground">{title}</p>
            <p className="text-xs text-muted-foreground break-words">{error.message}</p>
          </div>
          {onRetry && (
            <button
              onClick={onRetry}
              className="shrink-0 inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded border border-border bg-card hover:bg-accent transition-colors"
              aria-label="Retry"
            >
              <RefreshCw className="h-3 w-3" />
              Retry
            </button>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

export function EmptyState({ message, icon }: { message: string; icon?: React.ReactNode }) {
  return (
    <div className="flex flex-col items-center justify-center py-12 text-muted-foreground animate-fade-in">
      {icon && <div className="mb-3 opacity-40">{icon}</div>}
      <p className="text-sm">{message}</p>
    </div>
  )
}
