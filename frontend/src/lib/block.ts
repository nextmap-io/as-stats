import { isApiError } from "./api"

/**
 * blockErrorMessage turns a refused blackhole request into a sentence. Shared
 * by the Alerts page (POST /alerts/{id}/block) and Live Threats
 * (POST /bgp/blocks): 409 means BGP is off or only the no-op blocker is wired,
 * 422 that the target is not a single host.
 */
export function blockErrorMessage(err: Error, subject = "This alert"): string {
  if (isApiError(err, 409)) return "BGP blackholing is not enabled on this server."
  if (isApiError(err, 422)) return `${subject} does not target a single IP address, so there is nothing to blackhole.`
  if (isApiError(err, 403)) return "Only administrators can blackhole an address."
  return err.message
}
