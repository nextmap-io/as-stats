import type { ReactNode } from "react"
import { Link } from "react-router-dom"
import { useFeatures } from "@/hooks/useFeatures"
import type { Features } from "@/lib/types"

type Flag = Exclude<keyof Features, "auth" | "local_as">

/**
 * FeatureGate mounts a feature-gated page only when the server has the feature
 * on. The navigation already hides these pages, but a bookmark or a shared link
 * still lands on them; without the gate every query 404s and the page shows a
 * stack of "Not Found" errors (and live action buttons) instead of saying why.
 */
export function FeatureGate({
  flag,
  title,
  setting,
  children,
}: {
  flag: Flag
  /** Page title, kept so the operator knows where they landed. */
  title: string
  /** The server setting that turns the feature on. */
  setting: string
  children: ReactNode
}) {
  const { data, isLoading, isError } = useFeatures()
  // While the flags load, render nothing rather than a misleading message;
  // if they cannot load at all, let the page try and show its own errors.
  if (isLoading) return null
  if (isError || data?.data?.[flag]) return <>{children}</>
  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">{title}</h1>
      <div className="max-w-[70ch] border-t border-foreground pt-4 text-sm text-foreground-secondary">
        <p>
          This page is not enabled on this server. It needs <code>{setting}</code> in the API (and collector)
          environment, then a restart.
        </p>
        <p className="mt-2">
          <Link to="/" className="font-semibold underline underline-offset-2">
            Back to the dashboard
          </Link>
        </p>
      </div>
    </div>
  )
}
