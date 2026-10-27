import { Link } from "react-router-dom"

const ITEMS = [
  {
    to: "/changes",
    title: "Weekly traffic pattern",
    body: "The day × hour heatmap over 4 to 26 weeks, next to movers and new talkers in Network › Changes.",
  },
  {
    to: "/links",
    title: "Traffic by link, stacked",
    body: "Traffic stacked per link, IPv4, IPv6 or both, in Network › Links.",
  },
  {
    to: "/top/as",
    title: "Per-network charts",
    body: "Every AS name opens its own page with its traffic by link, peers and top IPs.",
  },
] as const

/** Plain links to the views that moved off the home page. */
export function FurtherAnalysis({ filterSearch }: { filterSearch: string }) {
  return (
    <ul className="grid grid-cols-1 gap-x-8 gap-y-4 md:grid-cols-3">
      {ITEMS.map((item) => (
        <li key={item.to}>
          <Link
            to={`${item.to}${filterSearch}`}
            className="font-semibold underline decoration-border underline-offset-[3px] hover:decoration-foreground"
          >
            {item.title}
          </Link>
          <p className="mt-0.5 text-[13px] text-muted-foreground">{item.body}</p>
        </li>
      ))}
    </ul>
  )
}
