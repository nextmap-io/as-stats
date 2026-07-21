/**
 * Country helpers — no dependency, no per-IP geo. The backend supplies the ISO
 * 3166-1 alpha-2 code (from as_names.country); everything here derives display
 * bits from that code alone.
 */

/**
 * Turn a 2-letter ISO 3166-1 alpha-2 code into its flag emoji by mapping each
 * A–Z letter to its Regional Indicator Symbol code point
 * (U+1F1E6 'A' … U+1F1FF 'Z'). Pure — no lookup table, no dependency.
 *
 * Returns the globe emoji for anything that isn't a clean 2-letter code
 * (empty, "Unknown", "XX", etc.) so callers always get a stable glyph width.
 */
export function countryFlag(code: string | undefined | null): string {
  if (!code) return "🌐"
  const cc = code.trim().toUpperCase()
  if (!/^[A-Z]{2}$/.test(cc)) return "🌐"
  const base = 0x1f1e6 // Regional Indicator Symbol Letter A
  const a = "A".charCodeAt(0)
  return String.fromCodePoint(base + (cc.charCodeAt(0) - a), base + (cc.charCodeAt(1) - a))
}

/** True when the code is a real 2-letter country code (not empty / "Unknown"). */
export function hasCountry(code: string | undefined | null): boolean {
  return !!code && /^[A-Za-z]{2}$/.test(code.trim())
}

/**
 * Best-effort human-readable name for a code. Prefers the backend-supplied
 * name; falls back to the platform's Intl display names, then to the raw code.
 */
export function countryName(code: string | undefined | null, supplied?: string): string {
  if (supplied) return supplied
  if (!hasCountry(code)) return code ? code : "Unknown"
  const cc = (code as string).trim().toUpperCase()
  try {
    const dn = new Intl.DisplayNames(["en"], { type: "region" })
    return dn.of(cc) ?? cc
  } catch {
    return cc
  }
}
