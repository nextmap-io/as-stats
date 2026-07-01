package reports

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"html/template"
	"strconv"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

// DataStore is the read surface the report generator needs. It is satisfied by
// *store.ClickHouseStore; declaring it as an interface keeps the generator
// unit-testable with a fake.
type DataStore interface {
	Overview(ctx context.Context, p store.QueryParams) (*model.Overview, error)
	TopAS(ctx context.Context, p store.QueryParams) ([]model.ASTraffic, uint64, error)
	TopCountry(ctx context.Context, p store.QueryParams) ([]model.CountryTraffic, uint64, error)
	LinksCapacity(ctx context.Context, from, to time.Time) ([]model.LinkCapacity, error)
	CountAlertsBySeverity(ctx context.Context) (map[string]uint64, error)
}

// topN caps how many rows each top-N section contributes to a report.
const topN = 20

// Rendered is the output of generating a report: an HTML body and a CSV
// attachment, plus the resolved window for logging / subject lines.
type Rendered struct {
	Subject  string
	HTML     []byte
	CSV      []byte
	From     time.Time
	To       time.Time
	Sections []string
}

// Generator renders reports from a DataStore.
type Generator struct {
	store DataStore
	tmpl  *template.Template
}

// NewGenerator builds a Generator with the HTML template compiled once.
func NewGenerator(s DataStore) (*Generator, error) {
	tmpl, err := template.New("report").Funcs(templateFuncs()).Parse(reportTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse report template: %w", err)
	}
	return &Generator{store: s, tmpl: tmpl}, nil
}

// reportData is the view model handed to the HTML template.
type reportData struct {
	ScheduleName string
	Frequency    string
	From         time.Time
	To           time.Time
	GeneratedAt  time.Time
	Sections     map[string]bool

	Overview   *model.Overview
	TopAS      []model.ASTraffic
	TopCountry []model.CountryTraffic
	Capacity   []model.LinkCapacity
	AlertRows  []alertRow
	AlertTotal uint64
}

type alertRow struct {
	Severity string
	Count    uint64
}

// Generate builds the HTML + CSV for a schedule evaluated at `at` (UTC). Only
// the schedule's sections are fetched and rendered.
func (g *Generator) Generate(ctx context.Context, s model.ReportSchedule, at time.Time) (Rendered, error) {
	from, to := windowFor(s.Frequency, at)
	set := sectionSet(s.Sections)

	data := reportData{
		ScheduleName: s.Name,
		Frequency:    s.Frequency,
		From:         from,
		To:           to,
		GeneratedAt:  time.Now().UTC(),
		Sections:     set,
	}

	p := store.DefaultQueryParams()
	p.From = from
	p.To = to
	p.Limit = topN

	if set["overview"] {
		ov, err := g.store.Overview(ctx, p)
		if err != nil {
			return Rendered{}, fmt.Errorf("overview: %w", err)
		}
		data.Overview = ov
	}
	if set["top_as"] {
		rows, _, err := g.store.TopAS(ctx, p)
		if err != nil {
			return Rendered{}, fmt.Errorf("top_as: %w", err)
		}
		data.TopAS = rows
	}
	if set["top_country"] {
		rows, _, err := g.store.TopCountry(ctx, p)
		if err != nil {
			return Rendered{}, fmt.Errorf("top_country: %w", err)
		}
		data.TopCountry = rows
	}
	if set["capacity"] {
		rows, err := g.store.LinksCapacity(ctx, from, to)
		if err != nil {
			return Rendered{}, fmt.Errorf("capacity: %w", err)
		}
		data.Capacity = rows
	}
	if set["alerts"] {
		counts, err := g.store.CountAlertsBySeverity(ctx)
		if err != nil {
			return Rendered{}, fmt.Errorf("alerts: %w", err)
		}
		for _, sev := range sortedAlertSeverities(counts) {
			data.AlertRows = append(data.AlertRows, alertRow{Severity: sev, Count: counts[sev]})
			data.AlertTotal += counts[sev]
		}
	}

	var htmlBuf bytes.Buffer
	if err := g.tmpl.Execute(&htmlBuf, data); err != nil {
		return Rendered{}, fmt.Errorf("render html: %w", err)
	}

	csvBytes, err := buildCSV(data)
	if err != nil {
		return Rendered{}, fmt.Errorf("build csv: %w", err)
	}

	secs, _ := ParseSections(s.Sections)
	return Rendered{
		Subject:  fmt.Sprintf("[AS-Stats] %s report — %s", s.Frequency, s.Name),
		HTML:     htmlBuf.Bytes(),
		CSV:      csvBytes,
		From:     from,
		To:       to,
		Sections: secs,
	}, nil
}

// buildCSV writes the enabled top-N tables into a single CSV buffer, each block
// preceded by a titled header row and separated by a blank line.
func buildCSV(d reportData) ([]byte, error) {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)

	writeRow := func(rec []string) error { return w.Write(rec) }
	blank := func() error { return w.Write([]string{}) }

	if d.Overview != nil {
		if err := writeRow([]string{"# Overview"}); err != nil {
			return nil, err
		}
		if err := writeRow([]string{"metric", "value"}); err != nil {
			return nil, err
		}
		rows := [][]string{
			{"total_bytes_in", u64(d.Overview.TotalBytesIn)},
			{"total_bytes_out", u64(d.Overview.TotalBytesOut)},
			{"total_flows", u64(d.Overview.TotalFlows)},
			{"active_as_count", u64(d.Overview.ActiveASCount)},
		}
		for _, r := range rows {
			if err := writeRow(r); err != nil {
				return nil, err
			}
		}
		if err := blank(); err != nil {
			return nil, err
		}
	}

	if len(d.TopAS) > 0 {
		if err := writeRow([]string{"# Top AS"}); err != nil {
			return nil, err
		}
		if err := writeRow([]string{"as_number", "as_name", "country", "bytes", "packets", "flows", "pct"}); err != nil {
			return nil, err
		}
		for _, a := range d.TopAS {
			if err := writeRow([]string{
				u32(a.ASNumber), a.ASName, a.Country,
				u64(a.Bytes), u64(a.Packets), u64(a.Flows), f2(a.Percent),
			}); err != nil {
				return nil, err
			}
		}
		if err := blank(); err != nil {
			return nil, err
		}
	}

	if len(d.TopCountry) > 0 {
		if err := writeRow([]string{"# Top Countries"}); err != nil {
			return nil, err
		}
		if err := writeRow([]string{"country", "name", "bytes", "packets", "flows", "pct"}); err != nil {
			return nil, err
		}
		for _, c := range d.TopCountry {
			if err := writeRow([]string{
				c.Country, c.Name, u64(c.Bytes), u64(c.Packets), u64(c.Flows), f2(c.Percent),
			}); err != nil {
				return nil, err
			}
		}
		if err := blank(); err != nil {
			return nil, err
		}
	}

	if len(d.Capacity) > 0 {
		if err := writeRow([]string{"# Link Capacity"}); err != nil {
			return nil, err
		}
		if err := writeRow([]string{"tag", "description", "capacity_mbps", "current_bps", "p95_bps", "utilization_pct"}); err != nil {
			return nil, err
		}
		for _, l := range d.Capacity {
			util := ""
			if l.UtilizationPct != nil {
				util = f2(*l.UtilizationPct)
			}
			if err := writeRow([]string{
				l.Tag, l.Description, u32(l.CapacityMbps), u64(l.CurrentBps), u64(l.P95Bps), util,
			}); err != nil {
				return nil, err
			}
		}
		if err := blank(); err != nil {
			return nil, err
		}
	}

	if len(d.AlertRows) > 0 {
		if err := writeRow([]string{"# Active Alerts"}); err != nil {
			return nil, err
		}
		if err := writeRow([]string{"severity", "count"}); err != nil {
			return nil, err
		}
		for _, a := range d.AlertRows {
			if err := writeRow([]string{a.Severity, u64(a.Count)}); err != nil {
				return nil, err
			}
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func u64(v uint64) string { return strconv.FormatUint(v, 10) }
func u32(v uint32) string { return strconv.FormatUint(uint64(v), 10) }
func f2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// templateFuncs provides display helpers for the HTML template.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"humanBytes": humanBytes,
		"humanBps":   humanBps,
		"pct":        func(v float64) string { return f2(v) + "%" },
		"utilPct": func(v *float64) string {
			if v == nil {
				return "—"
			}
			return f2(*v) + "%"
		},
		"fmtTime": func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") },
	}
}

// humanBytes renders a byte count as a human-readable string (base-1000).
func humanBytes(v uint64) string {
	return scale(float64(v), []string{"B", "kB", "MB", "GB", "TB", "PB"})
}

// humanBps renders a bits-per-second value (base-1000).
func humanBps(v uint64) string {
	return scale(float64(v), []string{"bps", "kbps", "Mbps", "Gbps", "Tbps"})
}

func scale(v float64, units []string) string {
	i := 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.2f %s", v, units[i])
}
