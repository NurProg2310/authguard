CREATE EXTENSION IF NOT EXISTS citext;

CREATE TABLE users (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    email               CITEXT      NOT NULL UNIQUE,
    name                TEXT        NOT NULL,
    password_hash       TEXT        NOT NULL,
    password_changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT users_email_length CHECK (char_length(email) BETWEEN 3 AND 254),
    CONSTRAINT users_name_length  CHECK (char_length(name) BETWEEN 1 AND 100)
);

CREATE TABLE sessions (
    id                    UUID        PRIMARY KEY,
    user_id               UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- SHA-256 of the opaque refresh-token secret; the plaintext is never stored.
    refresh_token_hash    BYTEA       NOT NULL,
    -- Hash of the immediately preceding refresh token, used to tell a benign
    -- concurrent-refresh race apart from refresh-token reuse (theft).
    previous_refresh_hash BYTEA,
    refreshed_at          TIMESTAMPTZ,
    user_agent            TEXT        NOT NULL DEFAULT '',
    ip_address            TEXT        NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at            TIMESTAMPTZ NOT NULL,
    revoked_at            TIMESTAMPTZ,
    revoke_reason         TEXT,
    CONSTRAINT sessions_revoke_reason CHECK (
        revoke_reason IS NULL OR revoke_reason IN
        ('signed_out', 'superseded', 'expired', 'password_reset', 'token_reuse')
    ),
    CONSTRAINT sessions_revoked_consistency CHECK ((revoked_at IS NULL) = (revoke_reason IS NULL))
);

-- The database-level guarantee behind the single-session policy: at most one
-- non-revoked session row may exist per user. Even if application-level
-- locking were bypassed, a second concurrent INSERT fails with 23505.
CREATE UNIQUE INDEX sessions_one_active_per_user ON sessions (user_id) WHERE revoked_at IS NULL;
CREATE INDEX sessions_user_created_idx ON sessions (user_id, created_at DESC);
CREATE INDEX sessions_revoked_at_idx ON sessions (revoked_at) WHERE revoked_at IS NOT NULL;

CREATE TABLE password_resets (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA       NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX password_resets_user_idx ON password_resets (user_id) WHERE used_at IS NULL;
CREATE INDEX password_resets_expires_idx ON password_resets (expires_at);
