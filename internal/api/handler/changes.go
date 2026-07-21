package handler

import (
	"net/http"
	"time"

	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/store"
)

// applyWindow overrides p.From/p.To with a [now-window, now] range when a
// "window" query param is present. It accepts either a preset token (1h, 3h,
// 6h, 24h, 7d, 30d) or any Go duration string (e.g. "90m", "48h"). Unknown
// values are ignored, leaving the from/to already parsed by parseQueryParams.
func applyWindow(r *http.Request, p *store.QueryParams) {
	v := r.URL.Query().Get("window")
	if v == "" {
		return
	}
	var d time.Duration
	switch v {
	case "1h":
		d = time.Hour
	case "3h":
		d = 3 * time.Hour
	case "6h":
		d = 6 * time.Hour
	case "24h":
		d = 24 * time.Hour
	case "7d":
		d = 7 * 24 * time.Hour
	case "30d":
		d = 30 * 24 * time.Hour
	default:
		parsed, err := time.ParseDuration(v)
		if err != nil || parsed <= 0 {
			return
		}
		d = parsed
	}
	now := time.Now().UTC()
	p.To = now
	p.From = now.Add(-d)
}

// Movers handles GET /api/v1/changes/movers?dimension=as|prefix|port|country
// &window=|from=&to=. Returns the biggest gainers and losers by absolute change
// in total bytes versus the immediately-prior equal-length window.
func (h *Handler) Movers(w http.ResponseWriter, r *http.Request) {
	dim := r.URL.Query().Get("dimension")
	if !store.SupportedMoverDimension(dim) {
		writeError(w, http.StatusBadRequest, "unknown dimension")
		return
	}
	if dim == "port" && !h.FeaturePortStats {
		writeError(w, http.StatusBadRequest, "port dimension requires FEATURE_PORT_STATS")
		return
	}

	p := parseQueryParams(r)
	applyWindow(r, &p)

	movers, err := h.Store.Movers(r.Context(), dim, p.From, p.To, p.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Split by sign: gainers (delta > 0) and losers (delta < 0). Movers is
	// already ranked by |delta| desc, so each slice stays ranked.
	gainers := make([]model.Mover, 0, len(movers))
	losers := make([]model.Mover, 0, len(movers))
	for _, m := range movers {
		switch {
		case m.Delta > 0:
			gainers = append(gainers, m)
		case m.Delta < 0:
			losers = append(losers, m)
		}
	}

	writeJSON(w, http.StatusOK, Response{
		Data: MoversResponse{Dimension: dim, Gainers: gainers, Losers: losers},
		Meta: &ResponseMeta{From: p.From, To: p.To, Limit: p.Limit},
	})
}

// Talkers handles GET /api/v1/changes/talkers?dimension=as|ip|prefix
// &window=|from=&to=. Returns entities that appeared or disappeared versus the
// immediately-prior equal-length window.
func (h *Handler) Talkers(w http.ResponseWriter, r *http.Request) {
	dim := r.URL.Query().Get("dimension")
	if !store.SupportedTalkerDimension(dim) {
		writeError(w, http.StatusBadRequest, "unknown dimension")
		return
	}

	p := parseQueryParams(r)
	applyWindow(r, &p)

	talkers, err := h.Store.Talkers(r.Context(), dim, p.From, p.To, p.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	appeared := make([]model.TalkerChange, 0, len(talkers))
	disappeared := make([]model.TalkerChange, 0, len(talkers))
	for _, t := range talkers {
		if t.Status == "new" {
			appeared = append(appeared, t)
		} else {
			disappeared = append(disappeared, t)
		}
	}

	writeJSON(w, http.StatusOK, Response{
		Data: TalkersResponse{Dimension: dim, New: appeared, Gone: disappeared},
		Meta: &ResponseMeta{From: p.From, To: p.To, Limit: p.Limit},
	})
}

// MoversResponse is the concrete payload for GET /changes/movers.
type MoversResponse struct {
	Dimension string        `json:"dimension"`
	Gainers   []model.Mover `json:"gainers"`
	Losers    []model.Mover `json:"losers"`
}

// TalkersResponse is the concrete payload for GET /changes/talkers.
type TalkersResponse struct {
	Dimension string               `json:"dimension"`
	New       []model.TalkerChange `json:"new"`
	Gone      []model.TalkerChange `json:"gone"`
}
