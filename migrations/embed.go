// Package migrations embeds the immutable ClickHouse migration files in the
// migration binary. Released images therefore always execute the SQL that was
// reviewed and built with that exact version of AS-Stats.
package migrations

import "embed"

// Files contains every forward migration. Existing migration files must never
// be edited after release: the migrator records and verifies their SHA-256.
//
//go:embed *.up.sql
var Files embed.FS
