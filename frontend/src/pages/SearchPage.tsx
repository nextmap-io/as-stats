import { Link, useSearchParams, Navigate } from "react-router-dom"
import { useSearch } from "@/hooks/useApi"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"

function detectRedirect(q: string): string | null {
  const asMatch = q.match(/^[Aa][Ss]?(\d+)$/)
  if (asMatch) return `/as/${asMatch[1]}`
  if (/^\d+$/.test(q)) return `/as/${q}`
  if (/^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}/.test(q) || (q.includes(":") && !q.includes(" ")))
    return `/ip/${encodeURIComponent(q.split("/")[0])}`
  return null
}

export function SearchPage() {
  const [searchParams] = useSearchParams()
  const q = searchParams.get("q") || ""
  const { data, isLoading, error } = useSearch(q)

  const redirect = detectRedirect(q)
  if (redirect) return <Navigate to={redirect} replace />

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold tracking-tight">
        Search: <span className="text-muted-foreground">{q}</span>
      </h1>

      <Card>
        <CardHeader className="pb-3">
          <CardTitle className="text-base">Results</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading && <p className="text-muted-foreground">Searching...</p>}
          {error && <p className="text-destructive">{error.message}</p>}
          {q.length < 2 && <p className="text-muted-foreground">Enter at least 2 characters to search</p>}

          {data?.data && (
            <>
              {data.data.length === 0 ? (
                <p className="text-muted-foreground">No results found</p>
              ) : (
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border">
                      <th className="pb-2 text-left font-medium text-muted-foreground">ASN</th>
                      <th className="pb-2 text-left font-medium text-muted-foreground">Name</th>
                      <th className="pb-2 text-left font-medium text-muted-foreground">Country</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.data.map(as => (
                      <tr key={as.number} className="border-b border-border/50 last:border-0 hover:bg-muted/50">
                        <td className="py-2">
                          <Link to={`/as/${as.number}`} className="text-primary hover:underline font-mono">
                            {as.number}
                          </Link>
                        </td>
                        <td className="py-2">{as.name}</td>
                        <td className="py-2 text-muted-foreground">{as.country || "-"}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
