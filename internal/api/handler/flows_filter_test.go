package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFlowSearchLongWindowNeedsSelectiveFilter(t *testing.T) {
	cases := []struct {
		query   string
		wantErr bool
	}{
		{"period=1h", false},
		{"period=24h", false},
		{"from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z", false}, // exactly 24 h
		{"period=7d", true},
		{"period=30d", true},
		{"period=7d&link=transit1&protocol=6&ip_version=4&min_bytes=1000", true},
		{"from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:01Z", true},
		{"period=7d&src_ip=192.0.2.1", false},
		{"period=7d&dst_ip=2001:db8::/48", false},
		{"period=30d&src_as=64500", false},
		{"period=30d&dst_as=64500", false},
		{"period=7d&src_port=53", false},
		{"period=7d&dst_port=443", false},
		{"period=7d&port=25", false},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/flows/search?"+tc.query, nil)
		_, err := parseFlowSearchFilters(r)
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.query, err, tc.wantErr)
		}
	}
}

// The 400 carries the message the Flow Search page displays; Store is nil, so
// the refusal must happen before any query.
func TestFlowSearchHandlersRefuseUnfilteredLongWindow(t *testing.T) {
	h := &Handler{FeatureFlowSearch: true}
	for name, fn := range map[string]http.HandlerFunc{"search": h.FlowSearch, "timeseries": h.FlowTimeSeries} {
		for _, q := range []string{"period=7d", "period=7d&format=csv"} {
			rec := httptest.NewRecorder()
			fn(rec, httptest.NewRequest(http.MethodGet, "/api/v1/flows/"+name+"?"+q, nil))
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s %s: status = %d, want 400", name, q, rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "need an IP or prefix, AS or port filter") {
				t.Errorf("%s %s: body = %s", name, q, rec.Body.String())
			}
		}
	}
}
