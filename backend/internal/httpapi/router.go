// Package httpapi exposes the authentication service over HTTP (Gin).
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/authguard/backend/internal/config"
	"github.com/authguard/backend/internal/gold"
	"github.com/authguard/backend/internal/service"
	"github.com/authguard/backend/internal/weather"
)

type Deps struct {
	Config  *config.Config
	Log     *slog.Logger
	Auth    *service.Auth
	DB      *pgxpool.Pool
	Redis   *redis.Client
	Weather *weather.Client
	Gold    *gold.Client
}

type handlers struct{ Deps }

func NewRouter(d Deps) (*gin.Engine, error) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	if err := r.SetTrustedProxies(d.Config.TrustedProxies); err != nil {
		return nil, err
	}
	r.RemoteIPHeaders = []string{"X-Real-IP", "X-Forwarded-For"}

	h := &handlers{d}
	r.Use(requestID(), requestLogger(d.Log), recovery(d.Log), apiHeaders(), bodyLimit(64<<10), requireCSRFHeader())
	r.NoRoute(func(c *gin.Context) { fail(c, http.StatusNotFound, "NOT_FOUND", "Route not found") })

	api := r.Group("/api")
	api.GET("/health", h.health)

	auth := api.Group("/auth")
	auth.POST("/register", h.rateLimit("register", 5, time.Hour), h.register)
	auth.POST("/login", h.rateLimit("login", 20, 15*time.Minute), h.login)
	auth.POST("/takeover", h.rateLimit("takeover", 10, 15*time.Minute), h.takeover)
	auth.POST("/refresh", h.rateLimit("refresh", 60, time.Minute), h.refresh)
	auth.POST("/logout", h.logout)
	auth.POST("/forgot-password", h.rateLimit("forgot", 5, 15*time.Minute), h.forgotPassword)
	auth.POST("/reset-password", h.rateLimit("reset", 10, 15*time.Minute), h.resetPassword)

	authed := api.Group("", h.requireAuth())
	authed.GET("/auth/me", h.me)
	authed.GET("/sessions", h.listSessions)
	authed.GET("/sessions/current", h.currentSession)
	authed.GET("/weather", h.rateLimit("weather", 60, time.Minute), h.weatherForecast)
	authed.GET("/weather/search", h.rateLimit("weather-search", 20, time.Minute), h.weatherSearch)
	authed.GET("/gold", h.rateLimit("gold", 30, time.Minute), h.goldPrices)
	authed.GET("/gold/history", h.rateLimit("gold-history", 30, time.Minute), h.goldHistory)

	return r, nil
}
