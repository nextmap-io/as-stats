package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestSetRetentionRejectsBadColdTier: an inconsistent cold tier is a 400
// answered before the store is touched (the handler has no store here).
func TestSetRetentionRejectsBadColdTier(t *testing.T) {
	h := &Handler{}
	for _, body := range []string{
		`{"ttl_days": 30, "enabled": true, "cold_after_days": 30}`,
		`{"ttl_days": 30, "enabled": true, "cold_after_days": 90}`,
		`{"ttl_days": 0, "enabled": true, "cold_after_days": 0}`,
	} {
		req := httptest.NewRequest(http.MethodPut, "/admin/retention/flows_log", strings.NewReader(body))
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("table", "flows_log")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		h.SetRetention(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, rec.Code)
		}
	}
}
