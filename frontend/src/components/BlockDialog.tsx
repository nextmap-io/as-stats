import { useEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { useQuery } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { blockDurations, blockErrorMessage, durationPhrase } from "@/lib/block"
import { Segmented } from "@/components/ui/segmented"

interface BlockDialogProps {
  /** Address (or prefix) to blackhole; null = closed. */
  target: string | null
  /** Pre-filled reason (alert name, Live Threats sentence…). */
  defaultReason?: string
  /** Used in error messages: "This alert", "This host". */
  subject?: string
  pending: boolean
  error: Error | null
  onConfirm: (durationMinutes: number, reason: string) => void
  onClose: () => void
}

/**
 * The one confirmation for a blackhole, used by the BGP page, Alerts and Live
 * Threats. It states the impact before anything else: a blackhole drops every
 * packet to the target upstream, legitimate traffic included.
 */
export function BlockDialog(props: BlockDialogProps) {
  if (!props.target) return null
  // Keyed on the target so the form starts fresh for every address.
  return <BlockDialogBody key={props.target} {...props} target={props.target} />
}

function BlockDialogBody({ target, defaultReason, subject, pending, error, onConfirm, onClose }: BlockDialogProps & { target: string }) {
  const { data: statusRes } = useQuery({ queryKey: ["bgp-status"], queryFn: () => api.bgpStatus(), staleTime: 10_000 })
  const st = statusRes?.data
  const durations = blockDurations(st?.max_duration_minutes ?? 1440)
  const fallback = st?.default_duration_minutes ?? 60
  const [duration, setDuration] = useState<number>(durations.some((d) => d.value === fallback) ? fallback : durations[0].value)
  const [reason, setReason] = useState(defaultReason ?? "")
  const confirmRef = useRef<HTMLButtonElement>(null)
  const onCloseRef = useRef(onClose)
  useEffect(() => {
    onCloseRef.current = onClose
  }, [onClose])

  useEffect(() => {
    confirmRef.current?.focus()
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onCloseRef.current()
    }
    document.addEventListener("keydown", handler)
    return () => document.removeEventListener("keydown", handler)
  }, [])

  return createPortal(
    <div className="fixed inset-0 z-[100] flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-labelledby="block-dialog-title">
      <button type="button" aria-label="Cancel" onClick={onClose} className="absolute inset-0 h-full w-full border-0 bg-black/50 p-0" />
      <form
        className="relative w-full max-w-lg rounded border border-border bg-card p-5 shadow-md"
        onSubmit={(e) => {
          e.preventDefault()
          onConfirm(duration, reason.trim())
        }}
      >
        <h2 id="block-dialog-title" className="text-[15px] font-semibold">
          Blackhole <span className="font-ip">{target}</span>?
        </h2>
        <p className="mt-2 text-sm">
          All traffic to <span className="font-ip font-semibold">{target}</span> will be dropped by your upstream providers —
          the attack <em>and</em> every legitimate visitor — until the block expires or is withdrawn. The address becomes
          unreachable from the Internet.
        </p>
        {st?.dry_run && (
          <p className="mt-2 border-l-2 border-warning-mark pl-3 text-[13px]">
            <span className="font-semibold text-warning">Dry run.</span> The server only records the block; no route is announced.
          </p>
        )}

        <div className="mt-4 space-y-1">
          <span id="block-duration-label" className="text-[13px] text-muted-foreground">
            Withdrawn automatically after
          </span>
          <div>
            <Segmented
              label="Block duration"
              options={durations.map((d) => ({ value: String(d.value), label: d.label }))}
              value={String(duration)}
              onChange={(v) => setDuration(Number(v))}
            />
          </div>
        </div>

        <label className="mt-3 block space-y-1">
          <span className="text-[13px] text-muted-foreground">Reason (kept in the history and the audit log)</span>
          <input
            type="text"
            value={reason}
            maxLength={500}
            onChange={(e) => setReason(e.target.value)}
            className="h-8 w-full rounded border border-input bg-background px-2 text-sm"
          />
        </label>

        {error && (
          <p role="alert" className="mt-3 border-l-2 border-destructive py-1 pl-3 text-sm">
            <span className="font-semibold">Not blocked.</span> {blockErrorMessage(error, subject)}
          </p>
        )}

        <div className="mt-5 flex flex-wrap justify-end gap-2">
          <button type="button" onClick={onClose} className="h-8 rounded border border-input bg-background px-3 text-sm hover:bg-accent">
            Cancel
          </button>
          <button
            ref={confirmRef}
            type="submit"
            disabled={pending}
            className="h-8 rounded border border-destructive bg-destructive px-3 text-sm font-semibold text-destructive-foreground hover:opacity-90 disabled:opacity-50"
          >
            {pending ? "Announcing…" : `Blackhole for ${durationPhrase(duration)}`}
          </button>
        </div>
      </form>
    </div>,
    document.body,
  )
}
