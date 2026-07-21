// Top-N page view mode (U9): table (default) | treemap | donut. Kept in its own
// module so the toggle component file only exports a component (Fast Refresh).
export type TopView = "table" | "treemap" | "donut"

/** Narrow the `?view=` URL param to a valid TopView (default "table"). */
export function asView(v: string | null | undefined): TopView {
  return v === "treemap" || v === "donut" ? v : "table"
}
