package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/authguard/backend/internal/service"
)

const refreshCookie = "ag_refresh"

func fail(c *gin.Context, status int, code, message string) {
	failWith(c, status, code, message, nil)
}

func failWith(c *gin.Context, status int, code, message string, extra gin.H) {
	body := gin.H{"code": code, "message": message}
	for k, v := range extra {
		body[k] = v
	}
	c.AbortWithStatusJSON(status, gin.H{"error": body})
}

func (h *handlers) internal(c *gin.Context, err error) {
	h.Log.Error("internal error", "error", err, "path", c.FullPath(), "request_id", c.GetString("request_id"))
	fail(c, http.StatusInternalServerError, "INTERNAL", "Something went wrong. Please try again.")
}

func (h *handlers) writeAuth(c *gin.Context, status int, r *service.AuthResult) {
	h.setRefreshCookie(c, r.RefreshToken, r.RefreshExpiresAt)
	c.JSON(status, gin.H{
		"user":         r.User,
		"access_token": r.AccessToken,
		"token_type":   "Bearer",
		"expires_at":   r.AccessExpiresAt,
		"session_id":   r.SessionID,
	})
}

func (h *handlers) setRefreshCookie(c *gin.Context, value string, exp time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshCookie,
		Value:    value,
		Path:     "/api/auth",
		Domain:   h.Config.CookieDomain,
		Expires:  exp,
		MaxAge:   int(time.Until(exp).Seconds()),
		HttpOnly: true,
		Secure:   h.Config.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *handlers) clearRefreshCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshCookie,
		Value:    "",
		Path:     "/api/auth",
		Domain:   h.Config.CookieDomain,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   h.Config.CookieSecure,
		SameSite: http.SameSiteStrictMode,
	})
}
