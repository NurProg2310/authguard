package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/authguard/backend/internal/security"
	"github.com/authguard/backend/internal/service"
)

const (
	ctxUserID    = "auth.user_id"
	ctxSessionID = "auth.session_id"
	csrfHeader   = "X-AuthGuard-CSRF"
)

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" || len(id) > 64 {
			id = uuid.NewString()
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

func requestLogger(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		status := c.Writer.Status()
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		}
		log.Log(c.Request.Context(), level, "request",
			"method", c.Request.Method,
			"path", c.FullPath(),
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
			"request_id", c.GetString("request_id"),
		)
	}
}

func recovery(log *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, err any) {
		log.Error("panic", "error", fmt.Sprint(err), "request_id", c.GetString("request_id"))
		fail(c, http.StatusInternalServerError, "INTERNAL", "Internal server error")
	})
}

func apiHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Next()
	}
}

func bodyLimit(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, n)
		c.Next()
	}
}

// requireCSRFHeader: every state-changing request must carry a custom header.
// Browsers can't attach custom headers cross-origin without a CORS preflight,
// which this API never approves — so cookie-authenticated endpoints (refresh,
// logout) cannot be triggered by a third-party site. Defence in depth on top of
// the SameSite=Strict refresh cookie.
func requireCSRFHeader() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if c.GetHeader(csrfHeader) != "1" {
				fail(c, http.StatusForbidden, "CSRF_REJECTED", "Missing CSRF protection header")
				return
			}
		}
		c.Next()
	}
}

func (h *handlers) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			fail(c, http.StatusUnauthorized, "MISSING_TOKEN", "Authentication required")
			return
		}
		claims, err := h.Auth.ParseAccessToken(raw)
		if errors.Is(err, security.ErrTokenExpired) {
			fail(c, http.StatusUnauthorized, "TOKEN_EXPIRED", "Access token expired")
			return
		}
		if err != nil {
			fail(c, http.StatusUnauthorized, "INVALID_TOKEN", "Invalid access token")
			return
		}
		userID, err1 := uuid.Parse(claims.Subject)
		sessionID, err2 := uuid.Parse(claims.SessionID)
		if err1 != nil || err2 != nil {
			fail(c, http.StatusUnauthorized, "INVALID_TOKEN", "Invalid access token")
			return
		}
		if err := h.Auth.ValidateSession(c.Request.Context(), sessionID, userID); err != nil {
			var revoked *service.SessionRevokedError
			if errors.As(err, &revoked) {
				failWith(c, http.StatusUnauthorized, "SESSION_REVOKED", "Your session is no longer active", gin.H{"reason": revoked.Reason})
				return
			}
			h.internal(c, err)
			return
		}
		c.Set(ctxUserID, userID)
		c.Set(ctxSessionID, sessionID)
		c.Next()
	}
}

// rateLimit is a fixed-window limiter keyed by client IP, backed by Redis.
// It fails open if Redis is unavailable (availability over strictness);
// nginx applies a coarse limit in front as well.
func (h *handlers) rateLimit(name string, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if ok, retry := h.allow(c, "ip:"+name+":"+c.ClientIP(), limit, window); !ok {
			tooMany(c, retry)
			return
		}
		c.Next()
	}
}

func (h *handlers) allow(c *gin.Context, key string, limit int, window time.Duration) (bool, time.Duration) {
	ctx := c.Request.Context()
	k := "ag:rl:" + key
	pipe := h.Redis.TxPipeline()
	incr := pipe.Incr(ctx, k)
	pipe.ExpireNX(ctx, k, window)
	ttl := pipe.TTL(ctx, k)
	if _, err := pipe.Exec(ctx); err != nil {
		h.Log.Warn("rate limiter unavailable", "error", err)
		return true, 0
	}
	return incr.Val() <= int64(limit), ttl.Val()
}

func tooMany(c *gin.Context, retry time.Duration) {
	secs := int(retry.Seconds())
	if secs < 1 {
		secs = 1
	}
	c.Header("Retry-After", fmt.Sprint(secs))
	failWith(c, http.StatusTooManyRequests, "RATE_LIMITED", "Too many attempts. Please try again later.", gin.H{"retry_after": secs})
}

func bearerToken(h string) (string, bool) {
	scheme, tok, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || tok == "" {
		return "", false
	}
	return strings.TrimSpace(tok), true
}
