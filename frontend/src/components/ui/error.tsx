import { AlertTriangle, RefreshCw, Inbox } from "lucide-react"
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
          <AlertTriangle className="size-5 text-destructive shrink-0 mt-0.5" />
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
              <RefreshCw className="size-3" />
              Retry
            </button>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

interface EmptyStateProps {
  /** Headline shown to the user. */
  message: string
  /** Optional secondary hint line under the headline. */
  hint?: string
  /** Optional leading icon. */
  icon?: React.ReactNode
  /** Optional call-to-action rendered below the text (e.g. a button). */
  action?: React.ReactNode
}

export function EmptyState({ message, hint, icon, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center py-12 text-center text-muted-foreground animate-fade-in">
      {icon && <div className="mb-3 opacity-40">{icon}</div>}
      <p className="text-sm">{message}</p>
      {hint && <p className="mt-1 text-xs text-muted-foreground/70">{hint}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}

/**
 * Reusable "no data in the selected time window" empty state — the common case
 * where a query succeeds but the current period simply has no rows. Prompts the
 * user to widen the range rather than implying an error.
 */
export function NoDataInWindow({ hint = "Try widening the time range.", action }: { hint?: string; action?: React.ReactNode }) {
  return (
    <EmptyState
      icon={<Inbox className="size-8" />}
      message="No data in this window"
      hint={hint}
      action={action}
    />
  )
}
