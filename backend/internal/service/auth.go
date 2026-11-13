// Package service implements authentication and the single-active-session policy.
//
// Invariants:
//   - PostgreSQL is the source of truth. A partial unique index guarantees at
//     most one non-revoked session per user, and every session mutation for a
//     user runs in a transaction that first takes a row lock on that user
//     (SELECT ... FOR UPDATE), serialising concurrent logins.
//   - Redis holds a fast "session is alive" key per session. Access tokens are
//     only accepted while that key exists, so deleting it invalidates every
//     outstanding access token for the session immediately. On a Redis miss we
//     fall back to PostgreSQL and re-populate, so Redis loss never grants access
//     to a revoked session and never locks out a valid one.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/authguard/backend/internal/mail"
	"github.com/authguard/backend/internal/security"
)

// refreshRaceWindow: a refresh presenting the *previous* token within this
// window is treated as a benign multi-tab race rather than token theft.
const refreshRaceWindow = 30 * time.Second

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidRefresh     = errors.New("invalid refresh token")
	ErrRefreshRace        = errors.New("refresh token already rotated")
	ErrInvalidTakeover    = errors.New("takeover token invalid or already used")
	ErrInvalidResetToken  = errors.New("reset token invalid or expired")
	ErrNotFound           = errors.New("not found")
)

// Revoke reasons (mirrors the CHECK constraint on sessions.revoke_reason).
const (
	ReasonSignedOut     = "signed_out"
	ReasonSuperseded    = "superseded"
	ReasonExpired       = "expired"
	ReasonPasswordReset = "password_reset"
	ReasonTokenReuse    = "token_reuse"
)

type ClientMeta struct {
	IP        string
	UserAgent string
}

type User struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type SessionInfo struct {
	ID           uuid.UUID  `json:"id"`
	UserAgent    string     `json:"user_agent"`
	IPAddress    string     `json:"ip_address"`
	CreatedAt    time.Time  `json:"created_at"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	RevokedAt    *time.Time `json:"revoked_at"`
	RevokeReason *string    `json:"revoke_reason"`
	Current      bool       `json:"current"`
}

type AuthResult struct {
	User             User
	SessionID        uuid.UUID
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

// SessionConflictError is returned when the user already has an active session.
// The caller has proven the password, so it receives a short-lived, single-use
// takeover token that lets it terminate the other session without re-sending
// credentials.
type SessionConflictError struct {
	Active            SessionInfo
	TakeoverToken     string
	TakeoverExpiresAt time.Time
}

func (e *SessionConflictError) Error() string { return "user already has an active session" }

type SessionRevokedError struct{ Reason string }

func (e *SessionRevokedError) Error() string { return "session revoked: " + e.Reason }

type Options struct {
	DB                 *pgxpool.Pool
	Redis              *redis.Client
	Tokens             *security.TokenManager
	Mailer             mail.Mailer
	Log                *slog.Logger
	RefreshTTL         time.Duration
	SessionMaxLifetime time.Duration
	TakeoverTTL        time.Duration
	ResetTTL           time.Duration
	AppURL             string
}

type Auth struct {
	Options
	dummyHash string
}

func NewAuth(o Options) (*Auth, error) {
	// Used to equalise timing between "unknown email" and "wrong password".
	h, err := security.HashPassword("authguard-timing-equaliser")
	if err != nil {
		return nil, err
	}
	return &Auth{Options: o, dummyHash: h}, nil
}

// ---------------------------------------------------------------- registration / login

func (a *Auth) Register(ctx context.Context, name, email, password string, meta ClientMeta) (*AuthResult, error) {
	hash, err := security.HashPassword(password)
	if err != nil {
		return nil, err
	}
	var u User
	err = a.DB.QueryRow(ctx,
		`INSERT INTO users (email, name, password_hash) VALUES ($1, $2, $3)
		 RETURNING id, email, name, created_at`,
		email, name, hash,
	).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt)
	if isUniqueViolation(err, "users_email_key") {
		return nil, ErrEmailTaken
	}
	if err != nil {
		return nil, err
	}
	return a.startSession(ctx, u, meta, false)
}

func (a *Auth) Login(ctx context.Context, email, password string, meta ClientMeta) (*AuthResult, error) {
	var u User
	var hash string
	err := a.DB.QueryRow(ctx,
		`SELECT id, email, name, created_at, password_hash FROM users WHERE email = $1`, email,
	).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = security.VerifyPassword(password, a.dummyHash)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	ok, err := security.VerifyPassword(password, hash)
	if err != nil {
		a.Log.Error("stored password hash unreadable", "user_id", u.ID, "error", err)
		return nil, ErrInvalidCredentials
	}
	if !ok {
		return nil, ErrInvalidCredentials
	}
	return a.startSession(ctx, u, meta, false)
}

// Takeover terminates the user's current session and starts a new one, using
// the single-use token handed out with a SessionConflictError.
func (a *Auth) Takeover(ctx context.Context, token string, meta ClientMeta) (*AuthResult, error) {
	claims, err := a.Tokens.ParseTakeover(token)
	if err != nil {
		return nil, ErrInvalidTakeover
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return nil, ErrInvalidTakeover
	}
	// Single use: the first SETNX wins; replays are rejected. Fail closed if Redis is down.
	fresh, err := a.Redis.SetNX(ctx, "ag:takeover-used:"+claims.ID, 1, a.TakeoverTTL+time.Minute).Result()
	if err != nil {
		return nil, fmt.Errorf("takeover replay check: %w", err)
	}
	if !fresh {
		return nil, ErrInvalidTakeover
	}

	var u User
	var pwChanged time.Time
	err = a.DB.QueryRow(ctx,
		`SELECT id, email, name, created_at, password_changed_at FROM users WHERE id = $1`, userID,
	).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt, &pwChanged)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidTakeover
	}
	if err != nil {
		return nil, err
	}
	// A password reset after the token was minted invalidates it.
	if claims.IssuedAt == nil || pwChanged.After(claims.IssuedAt.Add(time.Second)) {
		return nil, ErrInvalidTakeover
	}
	return a.startSession(ctx, u, meta, true)
}

// startSession is the heart of the single-session policy.
func (a *Auth) startSession(ctx context.Context, u User, meta ClientMeta, force bool) (*AuthResult, error) {
	sessionID := uuid.New()
	secret, err := security.RandomToken(32)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	refreshExp := minTime(now.Add(a.RefreshTTL), now.Add(a.SessionMaxLifetime))

	tx, err := a.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Lock the user row: concurrent logins for the same account queue here.
	var locked uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, u.ID).Scan(&locked); err != nil {
		return nil, err
	}

	// 2. A session past its expiry no longer counts as active.
	if _, err := tx.Exec(ctx,
		`UPDATE sessions SET revoked_at = now(), revoke_reason = $2
		 WHERE user_id = $1 AND revoked_at IS NULL AND expires_at <= now()`,
		u.ID, ReasonExpired,
	); err != nil {
		return nil, err
	}

	// 3. Is there still a live session?
	var active SessionInfo
	err = tx.QueryRow(ctx,
		`SELECT id, user_agent, ip_address, created_at, last_seen_at, expires_at
		 FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, u.ID,
	).Scan(&active.ID, &active.UserAgent, &active.IPAddress, &active.CreatedAt, &active.LastSeenAt, &active.ExpiresAt)
	hasActive := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	if hasActive {
		if !force {
			if err := tx.Commit(ctx); err != nil { // keep the expiry clean-up
				return nil, err
			}
			return nil, a.conflict(u.ID, active)
		}
		// 4. Takeover: revoke the old session inside the same transaction.
		if _, err := tx.Exec(ctx,
			`UPDATE sessions SET revoked_at = now(), revoke_reason = $2 WHERE id = $1`,
			active.ID, ReasonSuperseded,
		); err != nil {
			return nil, err
		}
		// Kill the fast-path key *before* commit so the old session stops
		// validating immediately; it's deleted again after commit to defeat a
		// concurrent DB-fallback re-populating it from pre-commit state.
		a.dropSessionCache(ctx, active.ID)
	}

	// 5. Create the new session. The partial unique index is the final backstop.
	_, err = tx.Exec(ctx,
		`INSERT INTO sessions (id, user_id, refresh_token_hash, user_agent, ip_address, created_at, last_seen_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $6, $7)`,
		sessionID, u.ID, security.HashToken(secret), truncate(meta.UserAgent, 512), meta.IP, now, refreshExp,
	)
	if isUniqueViolation(err, "sessions_one_active_per_user") {
		return nil, a.conflict(u.ID, active)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	if hasActive {
		a.dropSessionCache(ctx, active.ID)
		a.Log.Info("session superseded", "user_id", u.ID, "old_session", active.ID, "new_session", sessionID)
	}
	a.cacheSession(ctx, sessionID, u.ID, refreshExp)

	access, accessExp, err := a.Tokens.IssueAccess(u.ID, sessionID)
	if err != nil {
		return nil, err
	}
	return &AuthResult{
		User:             u,
		SessionID:        sessionID,
		AccessToken:      access,
		AccessExpiresAt:  accessExp,
		RefreshToken:     sessionID.String() + "." + secret,
		RefreshExpiresAt: refreshExp,
	}, nil
}

func (a *Auth) conflict(userID uuid.UUID, active SessionInfo) error {
	tok, exp, err := a.Tokens.IssueTakeover(userID)
	if err != nil {
		return err
	}
	return &SessionConflictError{Active: active, TakeoverToken: tok, TakeoverExpiresAt: exp}
}

// ---------------------------------------------------------------- refresh / logout

// Refresh rotates the refresh token. Presenting an older token outside the
// short race window is treated as theft and revokes the session.
func (a *Auth) Refresh(ctx context.Context, raw string, meta ClientMeta) (*AuthResult, error) {
	sessionID, secret, ok := splitRefreshToken(raw)
	if !ok {
		return nil, ErrInvalidRefresh
	}
	presented := security.HashToken(secret)
	now := time.Now().UTC()

	tx, err := a.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var (
		userID              uuid.UUID
		current, previous   []byte
		refreshedAt         *time.Time
		createdAt, expireAt time.Time
		revokedAt           *time.Time
		reason              *string
	)
	err = tx.QueryRow(ctx,
		`SELECT user_id, refresh_token_hash, previous_refresh_hash, refreshed_at, created_at, expires_at, revoked_at, revoke_reason
		 FROM sessions WHERE id = $1 FOR UPDATE`, sessionID,
	).Scan(&userID, &current, &previous, &refreshedAt, &createdAt, &expireAt, &revokedAt, &reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidRefresh
	}
	if err != nil {
		return nil, err
	}
	if revokedAt != nil {
		// Only disclose the reason to someone holding a once-valid token.
		if security.EqualHash(presented, current) || security.EqualHash(presented, previous) {
			return nil, &SessionRevokedError{Reason: deref(reason)}
		}
		return nil, ErrInvalidRefresh
	}

	revoke := func(why string) error {
		if _, err := tx.Exec(ctx,
			`UPDATE sessions SET revoked_at = now(), revoke_reason = $2 WHERE id = $1`, sessionID, why,
		); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		a.dropSessionCache(ctx, sessionID)
		return &SessionRevokedError{Reason: why}
	}

	switch {
	case security.EqualHash(presented, current):
		if !expireAt.After(now) {
			return nil, revoke(ReasonExpired)
		}
	case security.EqualHash(presented, previous) && refreshedAt != nil && now.Sub(*refreshedAt) < refreshRaceWindow:
		return nil, ErrRefreshRace
	case security.EqualHash(presented, previous):
		a.Log.Warn("refresh token reuse detected; revoking session", "session_id", sessionID, "ip", meta.IP)
		return nil, revoke(ReasonTokenReuse)
	default:
		return nil, ErrInvalidRefresh
	}

	newSecret, err := security.RandomToken(32)
	if err != nil {
		return nil, err
	}
	newExp := minTime(now.Add(a.RefreshTTL), createdAt.Add(a.SessionMaxLifetime))
	if _, err := tx.Exec(ctx,
		`UPDATE sessions SET previous_refresh_hash = refresh_token_hash, refresh_token_hash = $2,
		        refreshed_at = $3, last_seen_at = $3, expires_at = $4, ip_address = $5
		 WHERE id = $1`,
		sessionID, security.HashToken(newSecret), now, newExp, meta.IP,
	); err != nil {
		return nil, err
	}
	var u User
	if err := tx.QueryRow(ctx,
		`SELECT id, email, name, created_at FROM users WHERE id = $1`, userID,
	).Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	a.cacheSession(ctx, sessionID, userID, newExp)
	access, accessExp, err := a.Tokens.IssueAccess(userID, sessionID)
	if err != nil {
		return nil, err
	}
	return &AuthResult{
		User:             u,
		SessionID:        sessionID,
		AccessToken:      access,
		AccessExpiresAt:  accessExp,
		RefreshToken:     sessionID.String() + "." + newSecret,
		RefreshExpiresAt: newExp,
	}, nil
}

func (a *Auth) Logout(ctx context.Context, sessionID uuid.UUID) error {
	if _, err := a.DB.Exec(ctx,
		`UPDATE sessions SET revoked_at = now(), revoke_reason = $2 WHERE id = $1 AND revoked_at IS NULL`,
		sessionID, ReasonSignedOut,
	); err != nil {
		return err
	}
	a.dropSessionCache(ctx, sessionID)
	return nil
}

// SessionFromRefreshToken returns the session ID if raw is that session's current refresh token.
func (a *Auth) SessionFromRefreshToken(ctx context.Context, raw string) (uuid.UUID, bool) {
	sessionID, secret, ok := splitRefreshToken(raw)
	if !ok {
		return uuid.Nil, false
	}
	var current []byte
	if err := a.DB.QueryRow(ctx,
		`SELECT refresh_token_hash FROM sessions WHERE id = $1 AND revoked_at IS NULL`, sessionID,
	).Scan(&current); err != nil {
		return uuid.Nil, false
	}
	return sessionID, security.EqualHash(security.HashToken(secret), current)
}

// ---------------------------------------------------------------- session validation

func (a *Auth) ParseAccessToken(raw string) (*security.Claims, error) {
	return a.Tokens.ParseAccess(raw)
}

// ValidateSession is called on every authenticated request.
func (a *Auth) ValidateSession(ctx context.Context, sessionID, userID uuid.UUID) error {
	v, err := a.Redis.Get(ctx, sessionKey(sessionID)).Result()
	switch {
	case err == nil:
		if v == userID.String() {
			return nil
		}
		return &SessionRevokedError{Reason: "invalid"}
	case errors.Is(err, redis.Nil):
		// Not cached (revoked, or Redis restarted) -> consult the source of truth.
	default:
		a.Log.Warn("redis unavailable; validating session against postgres", "error", err)
	}

	var (
		owner     uuid.UUID
		revokedAt *time.Time
		reason    *string
		expiresAt time.Time
	)
	err = a.DB.QueryRow(ctx,
		`SELECT user_id, revoked_at, revoke_reason, expires_at FROM sessions WHERE id = $1`, sessionID,
	).Scan(&owner, &revokedAt, &reason, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return &SessionRevokedError{Reason: "invalid"}
	}
	if err != nil {
		return err
	}
	if owner != userID {
		return &SessionRevokedError{Reason: "invalid"}
	}
	if revokedAt != nil {
		return &SessionRevokedError{Reason: deref(reason)}
	}
	if !expiresAt.After(time.Now()) {
		return &SessionRevokedError{Reason: ReasonExpired}
	}
	a.cacheSession(ctx, sessionID, userID, expiresAt)
	return nil
}

func sessionKey(id uuid.UUID) string { return "ag:session:" + id.String() }

func (a *Auth) cacheSession(ctx context.Context, sessionID, userID uuid.UUID, exp time.Time) {
	ttl := time.Until(exp)
	if ttl <= 0 {
		return
	}
	if err := a.Redis.Set(ctx, sessionKey(sessionID), userID.String(), ttl).Err(); err != nil {
		a.Log.Warn("cache session failed", "session_id", sessionID, "error", err)
	}
}

func (a *Auth) dropSessionCache(ctx context.Context, sessionID uuid.UUID) {
	// Use a detached context: revocation must reach Redis even if the request was cancelled.
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := a.Redis.Del(c, sessionKey(sessionID)).Err(); err != nil {
		// Safe: ValidateSession falls back to postgres only on a cache miss, and a
		// stale positive entry can only remain if Redis is unreachable, in which
		// case reads also fail and fall through to postgres.
		a.Log.Error("drop session cache failed", "session_id", sessionID, "error", err)
	}
}

// ---------------------------------------------------------------- queries

func (a *Auth) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	var u User
	err := a.DB.QueryRow(ctx, `SELECT id, email, name, created_at FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

func (a *Auth) GetSession(ctx context.Context, sessionID uuid.UUID) (*SessionInfo, error) {
	var s SessionInfo
	err := a.DB.QueryRow(ctx,
		`SELECT id, user_agent, ip_address, created_at, last_seen_at, expires_at, revoked_at, revoke_reason
		 FROM sessions WHERE id = $1`, sessionID,
	).Scan(&s.ID, &s.UserAgent, &s.IPAddress, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.RevokedAt, &s.RevokeReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	s.Current = true
	return &s, err
}

func (a *Auth) ListSessions(ctx context.Context, userID, currentID uuid.UUID, limit int) ([]SessionInfo, error) {
	rows, err := a.DB.Query(ctx,
		`SELECT id, user_agent, ip_address, created_at, last_seen_at, expires_at, revoked_at, revoke_reason
		 FROM sessions WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SessionInfo, 0, limit)
	for rows.Next() {
		var s SessionInfo
		if err := rows.Scan(&s.ID, &s.UserAgent, &s.IPAddress, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.RevokedAt, &s.RevokeReason); err != nil {
			return nil, err
		}
		s.Current = s.ID == currentID
		out = append(out, s)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- password recovery

// ForgotPassword issues a reset link. It never reveals whether the email exists;
// callers should invoke it asynchronously so response timing doesn't either.
func (a *Auth) ForgotPassword(ctx context.Context, email string) error {
	var u User
	err := a.DB.QueryRow(ctx, `SELECT id, email, name FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Email, &u.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	token, err := security.RandomToken(32)
	if err != nil {
		return err
	}
	tx, err := a.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		`UPDATE password_resets SET used_at = now() WHERE user_id = $1 AND used_at IS NULL`, u.ID,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO password_resets (user_id, token_hash, expires_at) VALUES ($1, $2, $3)`,
		u.ID, security.HashToken(token), time.Now().Add(a.ResetTTL),
	); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	link := a.AppURL + "/reset-password?token=" + url.QueryEscape(token)
	mins := int(a.ResetTTL.Minutes())
	return a.Mailer.Send(ctx, mail.Message{
		To:      u.Email,
		Subject: "Reset your AuthGuard password",
		Text: fmt.Sprintf("Hi %s,\n\nUse the link below to reset your password. It expires in %d minutes.\n\n%s\n\n"+
			"If you didn't request this, you can ignore this email.\n", u.Name, mins, link),
		HTML: resetEmailHTML(u.Name, link, mins),
	})
}

// ResetPassword sets a new password and revokes every session for the user.
func (a *Auth) ResetPassword(ctx context.Context, token, newPassword string) error {
	hash, err := security.HashPassword(newPassword)
	if err != nil {
		return err
	}
	tx, err := a.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var resetID, userID uuid.UUID
	err = tx.QueryRow(ctx,
		`SELECT id, user_id FROM password_resets
		 WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now() FOR UPDATE`,
		security.HashToken(token),
	).Scan(&resetID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidResetToken
	}
	if err != nil {
		return err
	}
	// Same lock ordering as startSession (user row first) to avoid deadlocks.
	if _, err := tx.Exec(ctx,
		`UPDATE users SET password_hash = $2, password_changed_at = now(), updated_at = now() WHERE id = $1`,
		userID, hash,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE password_resets SET used_at = now() WHERE id = $1`, resetID); err != nil {
		return err
	}
	rows, err := tx.Query(ctx,
		`UPDATE sessions SET revoked_at = now(), revoke_reason = $2
		 WHERE user_id = $1 AND revoked_at IS NULL RETURNING id`, userID, ReasonPasswordReset)
	if err != nil {
		return err
	}
	revoked, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, id := range revoked {
		a.dropSessionCache(ctx, id)
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for _, id := range revoked {
		a.dropSessionCache(ctx, id)
	}
	a.Log.Info("password reset", "user_id", userID, "sessions_revoked", len(revoked))
	return nil
}

// ---------------------------------------------------------------- maintenance

// RunJanitor periodically expires stale sessions and prunes old rows.
func (a *Auth) RunJanitor(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		a.sweep(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (a *Auth) sweep(ctx context.Context) {
	stmts := []string{
		`UPDATE sessions SET revoked_at = now(), revoke_reason = 'expired' WHERE revoked_at IS NULL AND expires_at <= now()`,
		`DELETE FROM sessions WHERE revoked_at < now() - interval '90 days'`,
		`DELETE FROM password_resets WHERE expires_at < now() - interval '1 day'`,
	}
	for _, q := range stmts {
		if _, err := a.DB.Exec(ctx, q); err != nil && ctx.Err() == nil {
			a.Log.Error("janitor", "error", err)
		}
	}
}

// ---------------------------------------------------------------- helpers

func splitRefreshToken(raw string) (uuid.UUID, string, bool) {
	idPart, secret, ok := strings.Cut(raw, ".")
	if !ok || len(secret) < 32 {
		return uuid.Nil, "", false
	}
	id, err := uuid.Parse(idPart)
	if err != nil {
		return uuid.Nil, "", false
	}
	return id, secret, true
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && (constraint == "" || pgErr.ConstraintName == constraint)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
