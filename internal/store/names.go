package store

// as_names and links are ReplacingMergeTree tables: renaming an AS or editing a
// link (even just its colour) INSERTs a new row, and the old one only goes away
// at the next background merge. A plain `LEFT JOIN as_names` (ALL strictness)
// matches every unmerged version, so each fact row is duplicated and every
// sum() over it is inflated — verified on 24.3: AS 13335 read 100 → 200, and a
// colour edit doubled a link's traffic until the merge happened.
//
// Two rules keep that out of every read query:
//  1. Join the deduplicated views below, never the raw tables: one row per
//     key, newest version wins (argMax on updated_at).
//  2. Aggregate first, then join the names onto the (small) aggregated
//     result. Joining before GROUP BY makes the join run once per fact row.
//
// Both are plain SQL fragments with no user input; they are interpolated as a
// table expression: `LEFT JOIN ` + asNamesLatest + ` an ON …`.
const (
	asNamesLatest = `(
		SELECT as_number,
			argMax(as_name, updated_at) AS as_name,
			argMax(country, updated_at) AS country
		FROM as_names
		GROUP BY as_number
	)`

	linksLatest = `(
		SELECT tag,
			argMax(description, updated_at) AS description,
			argMax(capacity_mbps, updated_at) AS capacity_mbps
		FROM links
		GROUP BY tag
	)`
)
