package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/nextmap-io/as-stats/internal/model"
	"github.com/nextmap-io/as-stats/internal/reports"
)

// =============================================================================
// Scheduled reports CRUD (admin only, gated by FEATURE_REPORTS)
// =============================================================================

// ListReports handles GET /api/v1/admin/reports.
func (h *Handler) ListReports(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureReports {
		writeError(w, http.StatusNotFound, "reports feature disabled")
		return
	}
	schedules, err := h.Store.ListReportSchedules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: schedules})
}

// CreateReport handles POST /api/v1/admin/reports.
func (h *Handler) CreateReport(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureReports {
		writeError(w, http.StatusNotFound, "reports feature disabled")
		return
	}
	sched, err := decodeSchedule(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	applyScheduleDefaults(&sched)
	if err := reports.ValidateSchedule(sched); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if sched.ID == "" {
		sched.ID = uuid.NewString()
	}
	if err := h.Store.CreateReportSchedule(r.Context(), sched); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, Response{Data: sched})
}

// UpdateReport handles PUT /api/v1/admin/reports/{id}.
func (h *Handler) UpdateReport(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureReports {
		writeError(w, http.StatusNotFound, "reports feature disabled")
		return
	}
	id := chi.URLParam(r, "id")
	sched, err := decodeSchedule(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sched.ID = id
	applyScheduleDefaults(&sched)
	if err := reports.ValidateSchedule(sched); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Preserve created_at / last_run_at from the existing row.
	if existing, gerr := h.Store.GetReportSchedule(r.Context(), id); gerr == nil {
		sched.CreatedAt = existing.CreatedAt
		sched.LastRunAt = existing.LastRunAt
	}
	if err := h.Store.UpdateReportSchedule(r.Context(), sched); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: sched})
}

// DeleteReport handles DELETE /api/v1/admin/reports/{id}.
func (h *Handler) DeleteReport(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureReports {
		writeError(w, http.StatusNotFound, "reports feature disabled")
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.Store.DeleteReportSchedule(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TestReport handles POST /api/v1/admin/reports/{id}/test — renders and sends
// the report immediately, without touching last_run_at.
func (h *Handler) TestReport(w http.ResponseWriter, r *http.Request) {
	if !h.FeatureReports {
		writeError(w, http.StatusNotFound, "reports feature disabled")
		return
	}
	if h.ReportService == nil {
		writeError(w, http.StatusServiceUnavailable, "report delivery not configured (SMTP)")
		return
	}
	id := chi.URLParam(r, "id")
	sched, err := h.Store.GetReportSchedule(r.Context(), id)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			writeError(w, http.StatusNotFound, "report schedule not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.ReportService.SendNow(r.Context(), sched); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: map[string]string{"status": "sent"}})
}

// decodeSchedule reads a ReportSchedule from the request body with a size cap.
func decodeSchedule(w http.ResponseWriter, r *http.Request) (model.ReportSchedule, error) {
	var sched model.ReportSchedule
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	if err := json.NewDecoder(r.Body).Decode(&sched); err != nil {
		return model.ReportSchedule{}, errors.New("invalid JSON: " + err.Error())
	}
	return sched, nil
}

// applyScheduleDefaults fills in sensible defaults before validation.
func applyScheduleDefaults(s *model.ReportSchedule) {
	if s.Frequency == "" {
		s.Frequency = "daily"
	}
	if s.Format == "" {
		s.Format = "both"
	}
	if s.Frequency == "monthly" && s.DayOfMonth == 0 {
		s.DayOfMonth = 1
	}
	s.Recipients = strings.TrimSpace(s.Recipients)
	s.Sections = strings.TrimSpace(s.Sections)
}
