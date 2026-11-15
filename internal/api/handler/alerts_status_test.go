package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

func alertStatusRequest(id, action string) *http.Request {
	// The path is only cosmetic: the handler reads {id} from the route context.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/alerts/id/"+action, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// A malformed id is refused before any store access (Store is nil here: a
// lookup or mutation would panic).
func TestAlertStatusRejectsInvalidID(t *testing.T) {
	h := &Handler{FeatureAlerts: true}
	for _, id := range []string{"x", "1' OR 1=1", "00000000-0000-0000-0000", strings.Repeat("a", 64)} {
		for action, fn := range map[string]http.HandlerFunc{"ack": h.AcknowledgeAlert, "resolve": h.ResolveAlert} {
			rec := httptest.NewRecorder()
			fn(rec, alertStatusRequest(id, action))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s %q: status = %d, want 400", action, id, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "invalid alert id") {
				t.Errorf("%s %q: body = %s", action, id, rec.Body.String())
			}
		}
	}
}

func TestAlertStatusFeatureDisabled(t *testing.T) {
	h := &Handler{}
	rec := httptest.NewRecorder()
	h.AcknowledgeAlert(rec, alertStatusRequest(uuid.NewString(), "ack"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// Unknown ids are a 404 and leave the table alone; a stored alert is
// acknowledged then resolved.
func TestIntegrationAlertStatusEndpoints(t *testing.T) {
	addr := os.Getenv("ASSTATS_IT_CLICKHOUSE")
	if addr == "" {
		t.Skip("ASSTATS_IT_CLICKHOUSE not set")
	}
	ctx := context.Background()
	st, err := store.NewClickHouseStore(config.ClickHouseConfig{Addr: addr, Database: "asstats", User: "default"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	h := &Handler{FeatureAlerts: true, Store: st}

	for action, fn := range map[string]http.HandlerFunc{"ack": h.AcknowledgeAlert, "resolve": h.ResolveAlert} {
		rec := httptest.NewRecorder()
		fn(rec, alertStatusRequest(uuid.NewString(), action))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s unknown id: status = %d, want 404 (%s)", action, rec.Code, rec.Body.String())
		}
	}

	now := time.Now().UTC().Truncate(time.Second)
	id := uuid.NewString()
	if err := st.InsertAlert(ctx, model.Alert{
		ID: id, RuleID: uuid.NewString(), RuleName: "it-status", Severity: "warning",
		TriggeredAt: now, LastSeenAt: now, TargetIP: "192.0.2.77",
		MetricValue: 1, Threshold: 1, MetricType: "bps",
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := st.AlertExists(ctx, id); err != nil || !ok {
		t.Fatalf("AlertExists(inserted) = %v, %v", ok, err)
	}
	for _, step := range []struct {
		action string
		fn     http.HandlerFunc
	}{{"ack", h.AcknowledgeAlert}, {"resolve", h.ResolveAlert}} {
		rec := httptest.NewRecorder()
		// Upper case: the id is normalised before it reaches SQL.
		step.fn(rec, alertStatusRequest(strings.ToUpper(id), step.action))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200 (%s)", step.action, rec.Code, rec.Body.String())
		}
	}
}
