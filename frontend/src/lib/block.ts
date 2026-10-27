import { isApiError } from "./api"
import type { BGPBlock } from "./types"

/**
 * blockErrorMessage turns a refused blackhole request into a sentence. Shared
 * by the BGP page, the Alerts page (POST /alerts/{id}/block) and Live Threats
 * (POST /bgp/blocks). The server's own message is kept where it names the
 * reason (policy, rail, daemon): it says which setting refused the block.
 */
export function blockErrorMessage(err: Error, subject = "This alert"): string {
  if (isApiError(err, 409)) return "BGP blackholing is not enabled on this server."
  if (isApiError(err, 403)) return "Only administrators can blackhole an address."
  if (isApiError(err, 422)) {
    if (/single IP/i.test(err.message)) return `${subject} does not target a single IP address, so there is nothing to blackhole.`
    return `Refused by the safety rules: ${err.message}.`
  }
  if (isApiError(err, 429)) return `Limit reached: ${err.message}.`
  if (isApiError(err, 502)) return `The BGP daemon refused the command (${err.message.replace(/^BGP daemon command failed: /, "")}). Nothing was blackholed.`
  if (isApiError(err, 503)) return "The API has not loaded its BGP state yet (ClickHouse unreachable?). Try again in a few seconds."
  if (isApiError(err, 404)) return "No active block for this address."
  if (isApiError(err, 400)) return err.message
  return err.message
}

/** Duration presets (minutes), cut at the server's BGP_MAX_DURATION. */
export function blockDurations(maxMinutes = 1440): { value: number; label: string }[] {
  const all = [
    { value: 15, label: "15 min" },
    { value: 60, label: "1 hour" },
    { value: 240, label: "4 hours" },
    { value: 1440, label: "24 hours" },
  ]
  const out = all.filter((d) => d.value <= maxMinutes)
  return out.length > 0 ? out : [{ value: maxMinutes, label: `${maxMinutes} min` }]
}

/** The target as the operator should read it: an address, or address/len for a prefix block. */
export function blockTarget(b: Pick<BGPBlock, "ip" | "prefix_len">): string {
  const host = b.ip.includes(":") ? 128 : 32
  return b.prefix_len && b.prefix_len < host ? `${b.ip}/${b.prefix_len}` : b.ip
}

/** Who asked for the block, in words. */
export function blockSource(b: Pick<BGPBlock, "reason" | "rule_name">): string {
  switch (b.reason) {
    case "auto_block":
      return b.rule_name ? `Automatic, rule “${b.rule_name}”` : "Automatic"
    case "alert":
      return b.rule_name ? `From alert “${b.rule_name}”` : "From an alert"
    default:
      return "Manual"
  }
}

/** "in 42 min", "3 h ago"… for a timestamp relative to now. */
export function relativeTime(iso: string, now = Date.now()): string {
  const diff = new Date(iso).getTime() - now
  const abs = Math.abs(diff)
  let text: string
  if (abs < 60_000) text = "<1 min"
  else if (abs < 3_600_000) text = `${Math.round(abs / 60_000)} min`
  else if (abs < 86_400_000) text = `${Math.round(abs / 3_600_000)} h`
  else text = `${Math.round(abs / 86_400_000)} d`
  return diff >= 0 ? `in ${text}` : `${text} ago`
}

/** "1 hour", "4 hours", "15 minutes". */
export function durationPhrase(minutes: number): string {
  if (minutes % 60 === 0) {
    const h = minutes / 60
    return h === 1 ? "1 hour" : `${h} hours`
  }
  return minutes === 1 ? "1 minute" : `${minutes} minutes`
}
