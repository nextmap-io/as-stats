package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/nextmap-io/as-stats/internal/api/middleware"
)

// =============================================================================
// Read-only API tokens CRUD (Module G) — admin only + CSRF, always-on.
//
// Tokens grant viewer-role, GET/HEAD-only programmatic access via a Bearer
// header. The plaintext is returned exactly once on creation and never stored;
// list responses expose only the display prefix, never the hash or plaintext.
// =============================================================================

// ListTokens handles GET /api/v1/admin/tokens. Returns metadata for every token
// (including revoked ones, flagged via the Revoked field). Never returns the
// token hash or plaintext.
func (h *Handler) ListTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := h.Store.ListAPITokens(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, Response{Data: tokens})
}

// CreateToken handles POST /api/v1/admin/tokens. Mints a token and returns the
// one-time plaintext in the response. The owner defaults to the authenticated
// admin's email; expiry is optional (expires_in_days, 0/omitted = never).
func (h *Handler) CreateToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name          string `json:"name"`
		Owner         string `json:"owner"`
		ExpiresInDays int    `json:"expires_in_days"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(name) > 200 {
		writeError(w, http.StatusBadRequest, "name too long (max 200)")
		return
	}
	if body.ExpiresInDays < 0 || body.ExpiresInDays > 3650 {
		writeError(w, http.StatusBadRequest, "expires_in_days must be between 0 and 3650")
		return
	}

	// Owner: prefer the authenticated admin's identity; fall back to the request
	// body (e.g. when AUTH_ENABLED=false).
	owner := strings.TrimSpace(body.Owner)
	if user := middleware.GetUser(r.Context()); user != nil {
		if user.Email != "" {
			owner = user.Email
		} else if user.Sub != "" {
			owner = user.Sub
		}
	}

	var expiresAt time.Time
	if body.ExpiresInDays > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(body.ExpiresInDays) * 24 * time.Hour)
	}

	created, err := h.Store.CreateAPIToken(r.Context(), name, owner, expiresAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, Response{Data: created})
}

// revokeMutationTimeout bounds the wait for the revocation mutation to land.
// Generous, because reporting a credential dead before it is has to be the one
// outcome we never produce.
const revokeMutationTimeout = 30 * time.Second

// RevokeToken handles DELETE /api/v1/admin/tokens/{id}. Permanently disables a
// token; the operation is idempotent.
func (h *Handler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	// The id lands in an ALTER … WHERE id = @id against a UUID column; reject
	// non-UUIDs here so garbage is a 400 rather than a ClickHouse-level 500.
	if _, err := uuid.Parse(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid token id")
		return
	}

	// RevokeAPIToken issues an ALTER … UPDATE, which ClickHouse applies
	// asynchronously by default: the statement returns long before the part is
	// rewritten, and until then LookupAPIToken still sees revoked = 0 and keeps
	// authenticating the token. mutations_sync = 2 blocks until the mutation is
	// applied on every replica, so a 204 here means the credential really is
	// dead for the auth path; if it does not land we surface the failure instead
	// of claiming success.
	ctx, cancel := context.WithTimeout(r.Context(), revokeMutationTimeout)
	defer cancel()
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"mutations_sync": 2,
	}))

	if err := h.Store.RevokeAPIToken(ctx, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
