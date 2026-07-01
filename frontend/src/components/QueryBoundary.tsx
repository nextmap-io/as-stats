import type { ReactNode } from "react"
import { ErrorDisplay, NoDataInWindow } from "@/components/ui/error"
import { TableSkeleton } from "@/components/ui/skeleton"

/**
 * Minimal shape of a TanStack Query result that QueryBoundary needs. A full
 * `UseQueryResult<T, Error>` is structurally assignable to this, so callers can
 * pass the query object straight through without adapting it.
 */
export interface QueryBoundaryState<T> {
  isLoading: boolean
  isError: boolean
  error: Error | null
  data: T | undefined
  refetch?: () => unknown
}

interface QueryBoundaryProps<T> {
  /** The TanStack Query result (or a compatible {isLoading,isError,error,data}). */
  query: QueryBoundaryState<T>
  /** Render prop invoked with the resolved, non-empty data. */
  children: (data: NonNullable<T>) => ReactNode
  /**
   * Optional predicate deciding whether resolved data should be treated as
   * empty (e.g. `(d) => d.length === 0`). Undefined data is always empty.
   */
  isEmpty?: (data: NonNullable<T>) => boolean
  /** Custom loading placeholder. Defaults to a table skeleton. */
  skeleton?: ReactNode
  /** Rows for the default table skeleton. */
  loadingRows?: number
  /** Columns for the default table skeleton. */
  loadingCols?: number
  /** Title shown on the error panel. */
  errorTitle?: string
  /** Custom empty state. Defaults to the "no data in this window" variant. */
  empty?: ReactNode
}

/**
 * QueryBoundary renders consistent loading / error / empty / data states for a
 * TanStack Query result so pages don't hand-roll the same three branches.
 *
 * - loading  → skeleton
 * - error    → error panel with a Retry button wired to `refetch`
 * - empty    → empty state (widen-the-range hint by default)
 * - success  → `children(data)`
 */
export function QueryBoundary<T>({
  query,
  children,
  isEmpty,
  skeleton,
  loadingRows = 8,
  loadingCols = 5,
  errorTitle,
  empty,
}: QueryBoundaryProps<T>) {
  const { isLoading, isError, error, data, refetch } = query

  if (isLoading) {
    return <>{skeleton ?? <TableSkeleton rows={loadingRows} cols={loadingCols} />}</>
  }

  if (isError) {
    return (
      <ErrorDisplay
        error={error ?? new Error("Unknown error")}
        onRetry={refetch ? () => refetch() : undefined}
        title={errorTitle}
      />
    )
  }

  if (data === undefined || data === null) {
    return <>{empty ?? <NoDataInWindow />}</>
  }

  const resolved = data as NonNullable<T>
  if (isEmpty?.(resolved)) {
    return <>{empty ?? <NoDataInWindow />}</>
  }

  return <>{children(resolved)}</>
}
