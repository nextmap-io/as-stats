-- ============================================================================
-- Read-only API tokens (Module G) — api_tokens
-- ============================================================================
-- One row per minted API token. Tokens grant read-only (GET/HEAD) programmatic
-- access to the API via `Authorization: Bearer <token>`; they never carry write
-- privileges and always resolve to a viewer-role principal (see the auth
-- middleware). The plaintext token is shown to the operator exactly once at
-- creation time and is NEVER stored — only its SHA-256 hash and a short display
-- prefix are persisted, so a database leak cannot recover usable tokens.
--
-- ReplacingMergeTree(updated_at) so the latest edit (revoke, last-used touch)
-- always wins; ORDER BY id keeps one logical row per token. Always read with
-- FINAL. Always-created (like retention_policies) — no feature flag.
--
--   id            : UUID identifier (also used as the principal subject)
--   name          : human-friendly label shown in the admin UI
--   token_hash    : hex(sha256(plaintext)) — the only value used for lookup
--   token_prefix  : first ~10 chars of the plaintext, for display/identification
--   owner         : email/sub of the admin who minted the token
--   created_at    : mint time
--   last_used_at  : throttled ~1/min touch; toDateTime(0) = never used
--   expires_at    : toDateTime(0) = never expires
--   revoked       : 1 = permanently disabled (never accepted again)

CREATE TABLE IF NOT EXISTS asstats.api_tokens (
    id           String,
    name         String,
    token_hash   String,
    token_prefix String,
    owner        String,
    created_at   DateTime DEFAULT now(),
    last_used_at DateTime DEFAULT toDateTime(0),
    expires_at   DateTime DEFAULT toDateTime(0),
    revoked      UInt8 DEFAULT 0,
    updated_at   DateTime DEFAULT now()
) ENGINE = ReplacingMergeTree(updated_at)
ORDER BY id;
