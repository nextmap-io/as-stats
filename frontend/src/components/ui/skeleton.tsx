import { cn } from "@/lib/utils"

export function Skeleton({ className, style }: { className?: string; style?: React.CSSProperties }) {
  return <div className={cn("rounded bg-muted animate-shimmer", className)} style={style} />
}

export function TableSkeleton({ rows = 5, cols = 4 }: { rows?: number; cols?: number }) {
  return (
    <div className="space-y-3 animate-fade-in">
      <div className="flex gap-4">
        {Array.from({ length: cols }).map((_, i) => (
          <Skeleton key={i} className="h-4 flex-1" />
        ))}
      </div>
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="flex gap-4" style={{ animationDelay: `${i * 50}ms` }}>
          {Array.from({ length: cols }).map((_, j) => (
            <Skeleton key={j} className="h-5 flex-1" />
          ))}
        </div>
      ))}
    </div>
  )
}

export function CardSkeleton() {
  return (
    <div className="rounded-md border border-border bg-card p-6 space-y-4 animate-fade-in">
      <Skeleton className="h-5 w-32" />
      <Skeleton className="h-8 w-24" />
    </div>
  )
}

export function ChartSkeleton({ height = 300 }: { height?: number }) {
  return (
    <div className="rounded-md border border-border bg-card p-6 animate-fade-in">
      <Skeleton className="h-4 w-24 mb-4" />
      <Skeleton className="w-full" style={{ height }} />
    </div>
  )
}

export function PageSkeleton() {
  return (
    <div className="space-y-4">
      <div className="flex items-center gap-4">
        <Skeleton className="h-5 w-24" />
        <Skeleton className="h-4 w-16 ml-auto" />
        <Skeleton className="h-4 w-16" />
        <Skeleton className="h-4 w-12" />
        <Skeleton className="h-4 w-16" />
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <ChartSkeleton />
        <ChartSkeleton />
      </div>
      <Skeleton className="h-4 w-48" />
      <div className="space-y-3">
        {Array.from({ length: 3 }).map((_, i) => (
          <CardSkeleton key={i} />
        ))}
      </div>
    </div>
  )
}
