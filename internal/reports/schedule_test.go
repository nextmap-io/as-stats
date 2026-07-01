package reports

import (
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
)

var epoch = time.Unix(0, 0).UTC()

func ts(y int, m time.Month, d, h, min int) time.Time {
	return time.Date(y, m, d, h, min, 0, 0, time.UTC)
}

func TestIsDueDaily(t *testing.T) {
	s := model.ReportSchedule{Frequency: "daily", Hour: 9}

	cases := []struct {
		name    string
		now     time.Time
		lastRun time.Time
		want    bool
	}{
		{"first run after hour", ts(2026, 6, 30, 9, 30), epoch, true},
		{"already ran this occurrence", ts(2026, 6, 30, 9, 30), ts(2026, 6, 30, 9, 0), false},
		{"ran one minute before occurrence", ts(2026, 6, 30, 9, 30), ts(2026, 6, 30, 8, 59), true},
		{"before hour, last run was yesterday occurrence", ts(2026, 6, 30, 8, 30), ts(2026, 6, 29, 9, 0), false},
		{"exactly at occurrence, never ran", ts(2026, 6, 30, 9, 0), epoch, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isDue(s, c.now, c.lastRun); got != c.want {
				t.Fatalf("isDue=%v want %v (now=%s last=%s)", got, c.want, c.now, c.lastRun)
			}
		})
	}
}

func TestIsDueWeekly(t *testing.T) {
	// Anchor on a known Monday. 2026-06-29 is a Monday.
	monday := ts(2026, 6, 29, 9, 0)
	if monday.Weekday() != time.Monday {
		t.Fatalf("test anchor is %s, expected Monday", monday.Weekday())
	}
	s := model.ReportSchedule{Frequency: "weekly", Hour: 9, DayOfWeek: uint8(time.Monday)}

	cases := []struct {
		name    string
		now     time.Time
		lastRun time.Time
		want    bool
	}{
		{"monday after hour, never ran", ts(2026, 6, 29, 9, 30), epoch, true},
		{"monday, already ran", ts(2026, 6, 29, 9, 30), ts(2026, 6, 29, 9, 0), false},
		{"tuesday, ran monday", ts(2026, 6, 30, 10, 0), ts(2026, 6, 29, 9, 30), false},
		{"tuesday, ran previous week", ts(2026, 6, 30, 10, 0), ts(2026, 6, 22, 9, 0), true},
		{"monday before hour, ran last monday", ts(2026, 6, 29, 8, 0), ts(2026, 6, 22, 9, 0), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isDue(s, c.now, c.lastRun); got != c.want {
				t.Fatalf("isDue=%v want %v (now=%s last=%s)", got, c.want, c.now, c.lastRun)
			}
		})
	}
}

func TestIsDueMonthly(t *testing.T) {
	s := model.ReportSchedule{Frequency: "monthly", Hour: 0, DayOfMonth: 1}

	cases := []struct {
		name    string
		now     time.Time
		lastRun time.Time
		want    bool
	}{
		{"first of month after hour, never ran", ts(2026, 6, 1, 0, 30), epoch, true},
		{"first of month, already ran", ts(2026, 6, 1, 0, 30), ts(2026, 6, 1, 0, 0), false},
		{"mid month, ran previous month", ts(2026, 6, 15, 12, 0), ts(2026, 5, 20, 0, 0), true},
		{"mid month, ran this month", ts(2026, 6, 15, 12, 0), ts(2026, 6, 1, 0, 10), false},
		{"before dom occurrence crosses month boundary", ts(2026, 1, 1, 0, 30), ts(2025, 12, 1, 0, 0), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isDue(s, c.now, c.lastRun); got != c.want {
				t.Fatalf("isDue=%v want %v (now=%s last=%s)", got, c.want, c.now, c.lastRun)
			}
		})
	}
}

func TestIsDueUnknownFrequency(t *testing.T) {
	s := model.ReportSchedule{Frequency: "hourly", Hour: 9}
	if isDue(s, ts(2026, 6, 30, 9, 30), epoch) {
		t.Fatal("unknown frequency should never be due")
	}
}

func TestWindowFor(t *testing.T) {
	to := ts(2026, 6, 30, 12, 0)
	cases := map[string]time.Duration{
		"daily":   24 * time.Hour,
		"weekly":  7 * 24 * time.Hour,
		"monthly": 30 * 24 * time.Hour,
	}
	for freq, dur := range cases {
		from, gotTo := windowFor(freq, to)
		if !gotTo.Equal(to) {
			t.Fatalf("%s: to=%s want %s", freq, gotTo, to)
		}
		if got := gotTo.Sub(from); got != dur {
			t.Fatalf("%s: window=%s want %s", freq, got, dur)
		}
	}
}

func TestParseRecipients(t *testing.T) {
	ok, err := ParseRecipients(" a@b.com , c@d.com ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ok) != 2 || ok[0] != "a@b.com" || ok[1] != "c@d.com" {
		t.Fatalf("got %v", ok)
	}

	if _, err := ParseRecipients(""); err == nil {
		t.Fatal("empty recipients should error")
	}
	if _, err := ParseRecipients("not-an-email"); err == nil {
		t.Fatal("malformed recipient should error")
	}
	if _, err := ParseRecipients("good@x.com, bad@@x"); err == nil {
		t.Fatal("one bad recipient should fail the whole list")
	}
}

func TestParseSections(t *testing.T) {
	ok, err := ParseSections("overview, top_as ,overview")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ok) != 2 { // deduped
		t.Fatalf("expected 2 deduped sections, got %v", ok)
	}
	if _, err := ParseSections(""); err == nil {
		t.Fatal("empty sections should error")
	}
	if _, err := ParseSections("overview,bogus"); err == nil {
		t.Fatal("unknown section should error")
	}
}

func TestValidateSchedule(t *testing.T) {
	base := model.ReportSchedule{
		Name:       "Daily NOC",
		Frequency:  "daily",
		Hour:       6,
		Recipients: "noc@example.com",
		Sections:   "overview,top_as",
		Format:     "both",
	}
	if err := ValidateSchedule(base); err != nil {
		t.Fatalf("valid schedule rejected: %v", err)
	}

	bad := func(mut func(s *model.ReportSchedule)) model.ReportSchedule {
		s := base
		mut(&s)
		return s
	}

	cases := map[string]model.ReportSchedule{
		"empty name":        bad(func(s *model.ReportSchedule) { s.Name = "" }),
		"bad frequency":     bad(func(s *model.ReportSchedule) { s.Frequency = "hourly" }),
		"hour too large":    bad(func(s *model.ReportSchedule) { s.Hour = 24 }),
		"bad format":        bad(func(s *model.ReportSchedule) { s.Format = "pdf" }),
		"bad recipients":    bad(func(s *model.ReportSchedule) { s.Recipients = "nope" }),
		"bad sections":      bad(func(s *model.ReportSchedule) { s.Sections = "wat" }),
		"weekly bad dow":    bad(func(s *model.ReportSchedule) { s.Frequency = "weekly"; s.DayOfWeek = 7 }),
		"monthly bad dom0":  bad(func(s *model.ReportSchedule) { s.Frequency = "monthly"; s.DayOfMonth = 0 }),
		"monthly bad dom29": bad(func(s *model.ReportSchedule) { s.Frequency = "monthly"; s.DayOfMonth = 29 }),
	}
	for name, s := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateSchedule(s); err == nil {
				t.Fatalf("expected validation error for %q", name)
			}
		})
	}
}
