package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/authguard/backend/internal/service"
)

type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type takeoverRequest struct {
	TakeoverToken string `json:"takeover_token"`
}

type forgotRequest struct {
	Email string `json:"email"`
}

type resetRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (h *handlers) health(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	status := gin.H{"status": "ok", "postgres": "ok", "redis": "ok"}
	code := http.StatusOK
	if err := h.DB.Ping(ctx); err != nil {
		status["postgres"], status["status"], code = "down", "degraded", http.StatusServiceUnavailable
	}
	if err := h.Redis.Ping(ctx).Err(); err != nil {
		status["redis"], status["status"], code = "down", "degraded", http.StatusServiceUnavailable
	}
	c.JSON(code, status)
}

func (h *handlers) register(c *gin.Context) {
	var req registerRequest
	if !bindJSON(c, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = normalizeEmail(req.Email)
	fields := map[string]string{}
	if n := utf8.RuneCountInString(req.Name); n < 1 || n > 100 {
		fields["name"] = "Name must be between 1 and 100 characters"
	}
	if msg := validateEmail(req.Email); msg != "" {
		fields["email"] = msg
	}
	if msg := validatePassword(req.Password); msg != "" {
		fields["password"] = msg
	}
	if len(fields) > 0 {
		validationFailed(c, fields)
		return
	}
	res, err := h.Auth.Register(c.Request.Context(), req.Name, req.Email, req.Password, meta(c))
	if err != nil {
		h.authError(c, err)
		return
	}
	h.writeAuth(c, http.StatusCreated, res)
}

func (h *handlers) login(c *gin.Context) {
	var req loginRequest
	if !bindJSON(c, &req) {
		return
	}
	req.Email = normalizeEmail(req.Email)
	if req.Email == "" || req.Password == "" || len(req.Password) > 256 {
		fail(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid email or password")
		return
	}
	// Per-account limit slows credential stuffing spread across many IPs.
	if ok, retry := h.allow(c, "login-email:"+req.Email, 10, 15*time.Minute); !ok {
		tooMany(c, retry)
		return
	}
	res, err := h.Auth.Login(c.Request.Context(), req.Email, req.Password, meta(c))
	if err != nil {
		h.authError(c, err)
		return
	}
	h.writeAuth(c, http.StatusOK, res)
}

func (h *handlers) takeover(c *gin.Context) {
	var req takeoverRequest
	if !bindJSON(c, &req) {
		return
	}
	res, err := h.Auth.Takeover(c.Request.Context(), req.TakeoverToken, meta(c))
	if err != nil {
		h.authError(c, err)
		return
	}
	h.writeAuth(c, http.StatusOK, res)
}

func (h *handlers) refresh(c *gin.Context) {
	raw, err := c.Cookie(refreshCookie)
	if err != nil || raw == "" {
		fail(c, http.StatusUnauthorized, "NO_SESSION", "No active session")
		return
	}
	res, err := h.Auth.Refresh(c.Request.Context(), raw, meta(c))
	if err != nil {
		h.authError(c, err)
		return
	}
	h.writeAuth(c, http.StatusOK, res)
}

func (h *handlers) logout(c *gin.Context) {
	ctx := c.Request.Context()
	var sessionID uuid.UUID
	if raw, ok := bearerToken(c.GetHeader("Authorization")); ok {
		if claims, err := h.Auth.ParseAccessToken(raw); err == nil {
			sessionID, _ = uuid.Parse(claims.SessionID)
		}
	}
	if sessionID == uuid.Nil {
		if raw, err := c.Cookie(refreshCookie); err == nil {
			if id, ok := h.Auth.SessionFromRefreshToken(ctx, raw); ok {
				sessionID = id
			}
		}
	}
	if sessionID != uuid.Nil {
		if err := h.Auth.Logout(ctx, sessionID); err != nil {
			h.internal(c, err)
			return
		}
	}
	h.clearRefreshCookie(c)
	c.Status(http.StatusNoContent)
}

func (h *handlers) forgotPassword(c *gin.Context) {
	var req forgotRequest
	if !bindJSON(c, &req) {
		return
	}
	email := normalizeEmail(req.Email)
	if msg := validateEmail(email); msg != "" {
		validationFailed(c, map[string]string{"email": msg})
		return
	}
	if ok, _ := h.allow(c, "forgot-email:"+email, 3, time.Hour); ok {
		// Detached so the response time is identical whether or not the account exists.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := h.Auth.ForgotPassword(ctx, email); err != nil {
				h.Log.Error("forgot password", "error", err)
			}
		}()
	}
	c.JSON(http.StatusAccepted, gin.H{"message": "If an account exists for that email, a reset link is on its way."})
}

func (h *handlers) resetPassword(c *gin.Context) {
	var req resetRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Token == "" || len(req.Token) > 128 {
		fail(c, http.StatusBadRequest, "INVALID_RESET_TOKEN", "This reset link is invalid or has expired")
		return
	}
	if msg := validatePassword(req.Password); msg != "" {
		validationFailed(c, map[string]string{"password": msg})
		return
	}
	if err := h.Auth.ResetPassword(c.Request.Context(), req.Token, req.Password); err != nil {
		h.authError(c, err)
		return
	}
	h.clearRefreshCookie(c)
	c.JSON(http.StatusOK, gin.H{"message": "Password updated. Please sign in with your new password."})
}

func (h *handlers) me(c *gin.Context) {
	u, err := h.Auth.GetUser(c.Request.Context(), c.MustGet(ctxUserID).(uuid.UUID))
	if err != nil {
		h.internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u})
}

func (h *handlers) currentSession(c *gin.Context) {
	s, err := h.Auth.GetSession(c.Request.Context(), c.MustGet(ctxSessionID).(uuid.UUID))
	if err != nil {
		h.internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"session": s})
}

func (h *handlers) listSessions(c *gin.Context) {
	list, err := h.Auth.ListSessions(c.Request.Context(),
		c.MustGet(ctxUserID).(uuid.UUID), c.MustGet(ctxSessionID).(uuid.UUID), 15)
	if err != nil {
		h.internal(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": list})
}

// authError maps service errors to HTTP responses.
func (h *handlers) authError(c *gin.Context, err error) {
	var conflict *service.SessionConflictError
	var revoked *service.SessionRevokedError
	switch {
	case errors.As(err, &conflict):
		failWith(c, http.StatusConflict, "SESSION_ACTIVE",
			"You're already signed in on another device.",
			gin.H{
				"active_session":      conflict.Active,
				"takeover_token":      conflict.TakeoverToken,
				"takeover_expires_at": conflict.TakeoverExpiresAt,
			})
	case errors.As(err, &revoked):
		h.clearRefreshCookie(c)
		failWith(c, http.StatusUnauthorized, "SESSION_REVOKED", "Your session is no longer active", gin.H{"reason": revoked.Reason})
	case errors.Is(err, service.ErrInvalidCredentials):
		fail(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Invalid email or password")
	case errors.Is(err, service.ErrEmailTaken):
		validationFailedWith(c, http.StatusConflict, "EMAIL_TAKEN", map[string]string{"email": "An account with this email already exists"})
	case errors.Is(err, service.ErrInvalidRefresh):
		h.clearRefreshCookie(c)
		fail(c, http.StatusUnauthorized, "NO_SESSION", "No active session")
	case errors.Is(err, service.ErrRefreshRace):
		fail(c, http.StatusConflict, "REFRESH_RACE", "Session was refreshed concurrently; retry")
	case errors.Is(err, service.ErrInvalidTakeover):
		fail(c, http.StatusUnauthorized, "TAKEOVER_EXPIRED", "This sign-in attempt has expired. Please sign in again.")
	case errors.Is(err, service.ErrInvalidResetToken):
		fail(c, http.StatusBadRequest, "INVALID_RESET_TOKEN", "This reset link is invalid or has expired")
	default:
		h.internal(c, err)
	}
}

// ---------------------------------------------------------------- validation helpers

func bindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		fail(c, http.StatusBadRequest, "BAD_REQUEST", "Malformed JSON body")
		return false
	}
	return true
}

func validationFailed(c *gin.Context, fields map[string]string) {
	validationFailedWith(c, http.StatusUnprocessableEntity, "VALIDATION_ERROR", fields)
}

func validationFailedWith(c *gin.Context, status int, code string, fields map[string]string) {
	failWith(c, status, code, "Please correct the highlighted fields", gin.H{"fields": fields})
}

func normalizeEmail(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func validateEmail(e string) string {
	if e == "" {
		return "Email is required"
	}
	if len(e) > 254 {
		return "Email is too long"
	}
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e || !strings.Contains(e[strings.LastIndex(e, "@")+1:], ".") {
		return "Enter a valid email address"
	}
	return ""
}

func validatePassword(p string) string {
	n := utf8.RuneCountInString(p)
	if n < 8 {
		return "Password must be at least 8 characters"
	}
	if len(p) > 128 {
		return "Password must be at most 128 bytes"
	}
	var letter, digit bool
	for _, r := range p {
		letter = letter || unicode.IsLetter(r)
		digit = digit || unicode.IsDigit(r)
	}
	if !letter || !digit {
		return "Password must contain at least one letter and one number"
	}
	return ""
}

func meta(c *gin.Context) service.ClientMeta {
	return service.ClientMeta{IP: c.ClientIP(), UserAgent: c.Request.UserAgent()}
}
