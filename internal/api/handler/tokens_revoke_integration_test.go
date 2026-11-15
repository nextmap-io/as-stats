package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nextmap-io/as-stats/internal/config"
	"github.com/nextmap-io/as-stats/internal/store"
)

// A 204 from RevokeToken means the token no longer authenticates: the very
// next lookup must refuse it (the ALTER runs with mutations_sync = 2).
func TestIntegrationRevokeTokenIsImmediate(t *testing.T) {
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

	created, err := st.CreateAPIToken(ctx, "it-revoke", "it@example.invalid", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.LookupAPIToken(ctx, created.Token); err != nil {
		t.Fatalf("fresh token lookup: %v", err)
	}

	h := &Handler{Store: st}
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/admin/tokens/id", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", created.ID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.RevokeToken(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: status = %d (%s)", rec.Code, rec.Body.String())
	}
	if _, err := st.LookupAPIToken(ctx, created.Token); !errors.Is(err, store.ErrTokenNotFound) {
		t.Fatalf("lookup right after revoke: err = %v, want ErrTokenNotFound", err)
	}
}
