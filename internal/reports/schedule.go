// Package reports implements Module D scheduled reports: rendering an HTML
// summary + CSV over a frequency-derived window and delivering it via SMTP.
// Stdlib only (net/smtp, html/template, encoding/csv, mime/multipart).
package reports

import (
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

// ValidSections is the whitelist of report section keys. The HTML generator and
// CSV builder gate their output on this set; the handler validates against it.
var ValidSections = map[string]bool{
	"overview":    true,
	"top_as":      true,
	"top_country": true,
	"capacity":    true,
	"alerts":      true,
}

// validFrequencies is the whitelist of schedule frequencies.
var validFrequencies = map[string]bool{"daily": true, "weekly": true, "monthly": true}

// validFormats is the whitelist of delivery formats.
var validFormats = map[string]bool{"html": true, "csv": true, "both": true}

// ParseRecipients splits the comma-separated recipients field, trims each entry,
// and validates it as an RFC 5322 address. It returns the cleaned addresses or
// an error if the list is empty or any address is malformed.
func ParseRecipients(s string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if _, err := mail.ParseAddress(p); err != nil {
			return nil, fmt.Errorf("invalid recipient %q: %w", p, err)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one recipient is required")
	}
	return out, nil
}

// ParseSections splits the comma-separated sections field, trims each entry, and
// validates it against ValidSections. It returns the cleaned section keys or an
// error if the list is empty or contains an unknown key.
func ParseSections(s string) ([]string, error) {
	var out []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		if !ValidSections[p] {
			return nil, fmt.Errorf("unknown section %q", p)
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one section is required")
	}
	return out, nil
}

// sectionSet parses sections into a lookup set, ignoring errors (callers that
// need validation use ParseSections directly).
func sectionSet(s string) map[string]bool {
	set := make(map[string]bool)
	secs, err := ParseSections(s)
	if err != nil {
		return set
	}
	for _, sec := range secs {
		set[sec] = true
	}
	return set
}

// ValidateSchedule checks a schedule for well-formedness before persisting it.
func ValidateSchedule(s model.ReportSchedule) error {
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !validFrequencies[s.Frequency] {
		return fmt.Errorf("frequency must be daily|weekly|monthly")
	}
	if s.Hour > 23 {
		return fmt.Errorf("hour must be 0-23")
	}
	if s.Frequency == "weekly" && s.DayOfWeek > 6 {
		return fmt.Errorf("day_of_week must be 0-6")
	}
	if s.Frequency == "monthly" && (s.DayOfMonth < 1 || s.DayOfMonth > 28) {
		return fmt.Errorf("day_of_month must be 1-28")
	}
	if !validFormats[s.Format] {
		return fmt.Errorf("format must be html|csv|both")
	}
	if _, err := ParseRecipients(s.Recipients); err != nil {
		return err
	}
	if _, err := ParseSections(s.Sections); err != nil {
		return err
	}
	return nil
}

// windowFor returns the [from, to) reporting window for a schedule evaluated at
// `to`: daily → last 24h, weekly → last 7d, monthly → last 30d. Unknown
// frequencies fall back to 24h.
func windowFor(frequency string, to time.Time) (time.Time, time.Time) {
	to = to.UTC()
	switch frequency {
	case "weekly":
		return to.AddDate(0, 0, -7), to
	case "monthly":
		return to.AddDate(0, 0, -30), to
	default: // daily
		return to.Add(-24 * time.Hour), to
	}
}

// mostRecentOccurrence returns the most recent scheduled fire time at or before
// `now` (UTC) for the schedule. For daily this is today (or yesterday) at
// schedule.Hour; for weekly the most recent matching weekday at that hour; for
// monthly the most recent DayOfMonth at that hour. Returns the zero time for an
// unknown frequency.
func mostRecentOccurrence(s model.ReportSchedule, now time.Time) time.Time {
	now = now.UTC()
	h := int(s.Hour)
	switch s.Frequency {
	case "daily":
		occ := time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC)
		if occ.After(now) {
			occ = occ.AddDate(0, 0, -1)
		}
		return occ
	case "weekly":
		occ := time.Date(now.Year(), now.Month(), now.Day(), h, 0, 0, 0, time.UTC)
		offset := (int(now.Weekday()) - int(s.DayOfWeek) + 7) % 7
		occ = occ.AddDate(0, 0, -offset)
		if occ.After(now) {
			occ = occ.AddDate(0, 0, -7)
		}
		return occ
	case "monthly":
		dom := int(s.DayOfMonth)
		occ := time.Date(now.Year(), now.Month(), dom, h, 0, 0, 0, time.UTC)
		if occ.After(now) {
			// Roll back one month; dom is capped at 28 so this is always valid.
			occ = time.Date(now.Year(), now.Month()-1, dom, h, 0, 0, 0, time.UTC)
		}
		return occ
	default:
		return time.Time{}
	}
}

// isDue reports whether the schedule should fire at `now` given its last run.
// A schedule is due when its most-recent scheduled occurrence (at or before now)
// has not yet been run — i.e. lastRun is strictly before that occurrence. This
// is robust to collector restarts (a missed occurrence fires on the next tick)
// and dedupes repeated ticks within the same occurrence via lastRun. It is a
// pure function of its inputs. Unknown frequencies are never due.
func isDue(s model.ReportSchedule, now, lastRun time.Time) bool {
	occ := mostRecentOccurrence(s, now)
	if occ.IsZero() {
		return false
	}
	return lastRun.UTC().Before(occ)
}

// sortedAlertSeverities returns severities in a stable, most-severe-first order
// for deterministic report rendering.
func sortedAlertSeverities(counts map[string]uint64) []string {
	order := map[string]int{"critical": 0, "warning": 1, "info": 2}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		oi, oki := order[keys[i]]
		oj, okj := order[keys[j]]
		if oki && okj {
			return oi < oj
		}
		if oki != okj {
			return oki
		}
		return keys[i] < keys[j]
	})
	return keys
}
