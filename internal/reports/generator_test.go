package reports

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

func smtpTestConfig() config.SMTPConfig {
	return config.SMTPConfig{Host: "localhost", Port: 25, From: "as-stats@example.com", STARTTLS: false}
}

// fakeStore implements DataStore for generator tests.
type fakeStore struct {
	overview *model.Overview
	topAS    []model.ASTraffic
	country  []model.CountryTraffic
	capacity []model.LinkCapacity
	alerts   map[string]uint64
}

func (f *fakeStore) Overview(_ context.Context, _ store.QueryParams) (*model.Overview, error) {
	return f.overview, nil
}
func (f *fakeStore) TopAS(_ context.Context, _ store.QueryParams) ([]model.ASTraffic, uint64, error) {
	return f.topAS, 0, nil
}
func (f *fakeStore) TopCountry(_ context.Context, _ store.QueryParams) ([]model.CountryTraffic, uint64, error) {
	return f.country, 0, nil
}
func (f *fakeStore) LinksCapacity(_ context.Context, _, _ time.Time) ([]model.LinkCapacity, error) {
	return f.capacity, nil
}
func (f *fakeStore) CountAlertsBySeverity(_ context.Context) (map[string]uint64, error) {
	return f.alerts, nil
}

func TestGenerateAllSections(t *testing.T) {
	util := 42.5
	fs := &fakeStore{
		overview: &model.Overview{TotalBytesIn: 1234567, TotalBytesOut: 7654321, TotalFlows: 42, ActiveASCount: 7},
		topAS:    []model.ASTraffic{{ASNumber: 15169, ASName: "GOOGLE", Country: "US", Bytes: 999999, Percent: 55.5}},
		country:  []model.CountryTraffic{{Country: "US", Name: "United States", Bytes: 888888, Percent: 44.4}},
		capacity: []model.LinkCapacity{{Tag: "transit1", CapacityMbps: 10000, P95Bps: 5_000_000_000, UtilizationPct: &util}},
		alerts:   map[string]uint64{"critical": 2, "warning": 3},
	}
	gen, err := NewGenerator(fs)
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}

	sched := model.ReportSchedule{
		Name:      "Full",
		Frequency: "daily",
		Sections:  "overview,top_as,top_country,capacity,alerts",
		Format:    "both",
	}
	r, err := gen.Generate(context.Background(), sched, ts(2026, 6, 30, 9, 0))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	html := string(r.HTML)
	for _, want := range []string{"GOOGLE", "United States", "transit1", "Active Alerts (5)", "Overview"} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q", want)
		}
	}

	csvStr := string(r.CSV)
	for _, want := range []string{"# Overview", "# Top AS", "# Top Countries", "# Link Capacity", "# Active Alerts", "15169"} {
		if !strings.Contains(csvStr, want) {
			t.Errorf("CSV missing %q", want)
		}
	}
	if !bytes.Contains(r.HTML, []byte("<!DOCTYPE html>")) {
		t.Error("HTML missing doctype")
	}
	// Window: daily → 24h.
	if got := r.To.Sub(r.From); got != 24*time.Hour {
		t.Errorf("window=%s want 24h", got)
	}
}

func TestGenerateSubsetOnlyRendersRequested(t *testing.T) {
	fs := &fakeStore{
		overview: &model.Overview{TotalBytesIn: 1},
		topAS:    []model.ASTraffic{{ASNumber: 64500, ASName: "SHOULD-NOT-APPEAR"}},
	}
	gen, err := NewGenerator(fs)
	if err != nil {
		t.Fatalf("NewGenerator: %v", err)
	}
	sched := model.ReportSchedule{Name: "OverviewOnly", Frequency: "weekly", Sections: "overview", Format: "html"}
	r, err := gen.Generate(context.Background(), sched, ts(2026, 6, 30, 9, 0))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if strings.Contains(string(r.HTML), "SHOULD-NOT-APPEAR") {
		t.Error("top_as rendered despite not being requested")
	}
	if !strings.Contains(string(r.HTML), "Overview") {
		t.Error("overview section missing")
	}
	// CSV for html-only format is still produced (attachment handling is at send time).
	if len(r.CSV) == 0 {
		t.Error("expected non-empty CSV buffer")
	}
}

func TestBuildMessageFormats(t *testing.T) {
	s := NewSender(smtpTestConfig())
	rendered := Rendered{Subject: "Sub — dash", HTML: []byte("<p>hi</p>"), CSV: []byte("a,b\n1,2\n"), From: ts(2026, 6, 30, 0, 0)}

	for _, format := range []string{"html", "csv", "both"} {
		msg, err := s.buildMessage([]string{"x@y.com"}, rendered, format)
		if err != nil {
			t.Fatalf("format %s: %v", format, err)
		}
		m := string(msg)
		if !strings.Contains(m, "multipart/mixed") {
			t.Errorf("format %s: not multipart/mixed", format)
		}
		if !strings.Contains(m, "Subject: ") {
			t.Errorf("format %s: missing subject header", format)
		}
		wantAttachment := format == "csv" || format == "both"
		if got := strings.Contains(m, "attachment; filename="); got != wantAttachment {
			t.Errorf("format %s: attachment=%v want %v", format, got, wantAttachment)
		}
	}
}
