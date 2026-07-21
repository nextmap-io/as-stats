// detectRedirect maps a raw query (AS number, IP, or prefix) to the detail
// route it should navigate to, or null when the query isn't a direct entity
// reference (and should fall through to a text search). Shared by SearchPage
// and the command palette (U4) so both interpret raw input identically.
export function detectRedirect(q: string): string | null {
  const trimmed = q.trim()
  if (!trimmed) return null

  const asMatch = trimmed.match(/^[Aa][Ss]?(\d+)$/)
  if (asMatch) return `/as/${asMatch[1]}`
  if (/^\d+$/.test(trimmed)) return `/as/${trimmed}`
  if (
    /^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}/.test(trimmed) ||
    (trimmed.includes(":") && !trimmed.includes(" "))
  ) {
    return `/ip/${encodeURIComponent(trimmed.split("/")[0])}`
  }
  return null
}
