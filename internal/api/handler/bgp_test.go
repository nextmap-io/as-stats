package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nextmap-io/as-stats/internal/bgp"
)

func TestInternalAutoBlockRequiresTheToken(t *testing.T) {
	const token = "0123456789abcdef0123456789abcdef"
	h := &Handler{FeatureBGP: true, BGP: bgp.NewService(bgp.Options{}), BGPAPIToken: token}
	for name, header := range map[string]string{
		"none":       "",
		"wrong":      "Bearer 0123456789abcdef0123456789abcdeX",
		"prefix":     "Bearer " + token[:31],
		"basic":      "Basic " + token,
		"no scheme":  token,
		"extra char": "Bearer " + token + "x",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, bgp.InternalBlockPath, strings.NewReader(`{"ip":"198.51.100.10"}`))
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		h.InternalAutoBlock(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, rec.Code)
		}
	}
	// A short configured token never authenticates anything.
	h.BGPAPIToken = "short"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, bgp.InternalBlockPath, strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer short")
	h.InternalAutoBlock(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("short token: status %d", rec.Code)
	}
}

func TestWriteBGPError(t *testing.T) {
	cases := []struct {
		err  error
		code int
		body string
	}{
		{bgp.ErrInvalidTarget, http.StatusBadRequest, "invalid request"},
		{&bgp.PolicyError{Reason: "outside the allowed prefixes"}, http.StatusUnprocessableEntity, "outside the allowed prefixes"},
		{&bgp.LimitError{Reason: "BGP_MAX_ACTIVE_BLOCKS"}, http.StatusTooManyRequests, "BGP_MAX_ACTIVE_BLOCKS"},
		{bgp.ErrNotFound, http.StatusNotFound, "no active block"},
		{bgp.ErrNotReady, http.StatusServiceUnavailable, "not loaded"},
		{&bgp.CommandError{Argv: []string{"gobgp", "global"}, Err: errors.New("exit status 1"), Output: "secret-ish output"}, http.StatusBadGateway, "gobgp failed: exit status 1"},
		{errors.New("clickhouse: boom"), http.StatusInternalServerError, "internal server error"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		writeBGPError(rec, c.err)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.body) {
			t.Errorf("%v: %d %s, want %d containing %q", c.err, rec.Code, rec.Body.String(), c.code, c.body)
		}
		if strings.Contains(rec.Body.String(), "secret-ish") {
			t.Errorf("command output leaked to the client: %s", rec.Body.String())
		}
	}
}

func TestCreateBGPBlockConflictWhenDisabled(t *testing.T) {
	h := &Handler{FeatureBGP: false}
	for _, fn := range []http.HandlerFunc{h.CreateBGPBlock, h.DeleteBGPBlock} {
		rec := httptest.NewRecorder()
		fn(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"ip":"198.51.100.1"}`)))
		if rec.Code != http.StatusConflict {
			t.Errorf("status %d, want 409", rec.Code)
		}
	}
}
