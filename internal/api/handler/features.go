package handler

import "net/http"

// Features handles GET /api/v1/features — returns which features are enabled.
// Used by the frontend to conditionally render UI elements.
type FeaturesResponse struct {
	FlowSearch bool   `json:"flow_search"`
	PortStats  bool   `json:"port_stats"`
	Alerts     bool   `json:"alerts"`
	LocalAS    uint32 `json:"local_as,omitempty"`
	Auth       bool   `json:"auth"`
}

func (h *Handler) Features(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, Response{
		Data: FeaturesResponse{
			FlowSearch: h.FeatureFlowSearch,
			PortStats:  h.FeaturePortStats,
			Alerts:     h.FeatureAlerts,
			LocalAS:    h.LocalAS,
		},
	})
}
