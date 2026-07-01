package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/nextmap-io/as-stats/internal/model"
)

// =============================================================================
// Read-only API tokens (Module G)
//
// Tokens are minted with a 32-byte crypto/rand body, hex-encoded and prefixed
// with "ast_". Only the SHA-256 hash and a short display prefix are stored — the
// plaintext is returned to the caller exactly once and never persisted, so a DB
// leak cannot recover usable tokens. Table is ReplacingMergeTree(updated_at),
// always read with FINAL. See migration 000014_api_tokens.
// =============================================================================

// apiTokenPrefix is prepended to every minted token so the auth middleware can
// cheaply distinguish an API token from a legacy session-id bearer.
const apiTokenPrefix = "ast_"

// apiTokenRandomBytes is the entropy of the token body (before hex-encoding).
const apiTokenRandomBytes = 32

// apiTokenDisplayLen is how many leading characters of the plaintext are kept
// as token_prefix for display/identification in the admin UI.
const apiTokenDisplayLen = 10

// ErrTokenNotFound is returned by LookupAPIToken when no active, non-revoked,
// non-expired token matches the supplied plaintext.
var ErrTokenNotFound = errors.New("api token not found")

const apiTokenColumns = `id, name, token_prefix, owner, created_at, last_used_at, expires_at, revoked, updated_at`

// scanAPIToken scans a single row in apiTokenColumns order. token_hash is never
// selected, so it can never leak into a model / API response.
func scanAPIToken(row interface {
	Scan(dest ...any) error
}) (model.APIToken, error) {
	var t model.APIToken
	var revoked uint8
	if err := row.Scan(
		&t.ID, &t.Name, &t.TokenPrefix, &t.Owner,
		&t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt, &revoked, &t.UpdatedAt,
	); err != nil {
		return model.APIToken{}, err
	}
	t.Revoked = revoked == 1
	return t, nil
}

// generateAPIToken produces a fresh plaintext token ("ast_" + 64 hex chars),
// its SHA-256 hex hash, and the display prefix. Pure aside from crypto/rand, so
// it is unit-testable in isolation.
func generateAPIToken() (plaintext, hash, prefix string, err error) {
	b := make([]byte, apiTokenRandomBytes)
	if _, err = rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("generate api token: %w", err)
	}
	plaintext = apiTokenPrefix + hex.EncodeToString(b)
	hash = hashAPIToken(plaintext)
	prefix = plaintext
	if len(prefix) > apiTokenDisplayLen {
		prefix = prefix[:apiTokenDisplayLen]
	}
	return plaintext, hash, prefix, nil
}

// hashAPIToken returns the hex-encoded SHA-256 of a plaintext token. This is the
// only value used to look a token up; it is a pure function for easy testing.
func hashAPIToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// CreateAPIToken mints a new read-only token. It returns the record plus the
// PLAINTEXT token, which the caller must surface to the operator immediately —
// it is never recoverable afterwards. expiresAt with Unix() <= 0 means "never".
func (s *ClickHouseStore) CreateAPIToken(ctx context.Context, name, owner string, expiresAt time.Time) (model.APITokenCreated, error) {
	plaintext, hash, prefix, err := generateAPIToken()
	if err != nil {
		return model.APITokenCreated{}, err
	}

	now := time.Now().UTC()
	if expiresAt.IsZero() || expiresAt.Unix() < 0 {
		expiresAt = time.Unix(0, 0).UTC()
	}
	rec := model.APIToken{
		ID:          uuid.NewString(),
		Name:        name,
		TokenPrefix: prefix,
		Owner:       owner,
		CreatedAt:   now,
		LastUsedAt:  time.Unix(0, 0).UTC(),
		ExpiresAt:   expiresAt.UTC(),
		Revoked:     false,
		UpdatedAt:   now,
	}

	if err := s.conn.Exec(ctx, `
		INSERT INTO api_tokens
			(id, name, token_hash, token_prefix, owner, created_at, last_used_at, expires_at, revoked, updated_at)
		VALUES
			(@id, @name, @token_hash, @token_prefix, @owner, @created_at, @last_used_at, @expires_at, 0, @updated_at)
	`,
		clickhouse.Named("id", rec.ID),
		clickhouse.Named("name", rec.Name),
		clickhouse.Named("token_hash", hash),
		clickhouse.Named("token_prefix", rec.TokenPrefix),
		clickhouse.Named("owner", rec.Owner),
		clickhouse.Named("created_at", rec.CreatedAt),
		clickhouse.Named("last_used_at", rec.LastUsedAt),
		clickhouse.Named("expires_at", rec.ExpiresAt),
		clickhouse.Named("updated_at", rec.UpdatedAt),
	); err != nil {
		return model.APITokenCreated{}, fmt.Errorf("insert api token: %w", err)
	}

	return model.APITokenCreated{APIToken: rec, Token: plaintext}, nil
}

// ListAPITokens returns every token (including revoked ones, flagged via the
// Revoked field so the UI can show status), ordered by creation time descending.
// token_hash is never selected.
func (s *ClickHouseStore) ListAPITokens(ctx context.Context) ([]model.APIToken, error) {
	query := fmt.Sprintf(`
		SELECT %s
		FROM api_tokens FINAL
		ORDER BY created_at DESC
	`, apiTokenColumns)
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list api tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []model.APIToken
	for rows.Next() {
		t, err := scanAPIToken(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, t)
	}
	return results, nil
}

// RevokeAPIToken permanently disables a token. Uses a lightweight in-place
// mutation so a concurrent last-used touch cannot resurrect the row. Once
// revoked, LookupAPIToken will never return it again.
func (s *ClickHouseStore) RevokeAPIToken(ctx context.Context, id string) error {
	return s.conn.Exec(ctx,
		"ALTER TABLE api_tokens UPDATE revoked = 1, updated_at = now() WHERE id = @id",
		clickhouse.Named("id", id),
	)
}

// LookupAPIToken resolves a plaintext token to its record, enforcing the
// revoked/expiry gate in SQL. Returns ErrTokenNotFound when no usable token
// matches. The plaintext is hashed locally; only the hash touches the database.
func (s *ClickHouseStore) LookupAPIToken(ctx context.Context, plaintext string) (model.APIToken, error) {
	hash := hashAPIToken(plaintext)
	query := fmt.Sprintf(`
		SELECT %s
		FROM api_tokens FINAL
		WHERE token_hash = @h
		  AND revoked = 0
		  AND (expires_at = toDateTime(0) OR expires_at > now())
		LIMIT 1
	`, apiTokenColumns)
	row := s.conn.QueryRow(ctx, query, clickhouse.Named("h", hash))
	t, err := scanAPIToken(row)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return model.APIToken{}, ErrTokenNotFound
		}
		return model.APIToken{}, fmt.Errorf("lookup api token: %w", err)
	}
	return t, nil
}

// TouchAPIToken updates last_used_at for a token. Called (throttled, async) by
// the auth middleware; failures are non-fatal for the request. An in-place
// mutation on a non-key column keeps the revoked flag intact.
func (s *ClickHouseStore) TouchAPIToken(ctx context.Context, id string, ts time.Time) error {
	return s.conn.Exec(ctx,
		"ALTER TABLE api_tokens UPDATE last_used_at = @ts WHERE id = @id",
		clickhouse.Named("id", id),
		clickhouse.Named("ts", ts.UTC()),
	)
}
