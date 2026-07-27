package store

import (
	"fmt"
	"strings"

	"github.com/nextmap-io/as-stats/internal/model"
)

// groupedAS wraps an ASN column/expression in the private-use collapse: every
// ASN inside a private range folds into model.PrivateASGroup, everything else
// passes through untouched. Applying it to the grouping key *before* GROUP BY
// is what turns a dozen "-Private Use AS-" rows into a single one while leaving
// sum()/ORDER BY/LIMIT semantics — and therefore totals and percentages —
// exactly as they were.
//
// col must be a column reference or expression the caller controls (a table
// alias qualified name such as `t.as_number`), never user input: it is
// interpolated verbatim. The numeric bounds come from internal/model so the
// SQL can never drift from model.IsPrivateAS.
func groupedAS(col string) string {
	return fmt.Sprintf("if((%[1]s >= %[2]d AND %[1]s <= %[3]d) OR (%[1]s >= %[4]d AND %[1]s <= %[5]d), %[6]d, %[1]s)",
		col,
		model.PrivateAS16Min, model.PrivateAS16Max,
		model.PrivateAS32Min, model.PrivateAS32Max,
		model.PrivateASGroup,
	)
}

// groupedASName labels the collapsed row. as_names never holds a row for the
// synthetic ASN (and holds nothing useful for the private ones that fed it), so
// a LEFT JOIN would leave the group nameless in the UI; substitute the fixed
// display name instead. asExpr must be the same expression groupedAS produced
// for the grouping key, nameExpr the joined-name expression it replaces.
func groupedASName(asExpr, nameExpr string) string {
	return fmt.Sprintf("if(%s = %d, '%s', %s)",
		asExpr, model.PrivateASGroup, strings.ReplaceAll(model.PrivateASName, "'", "\\'"), nameExpr)
}
