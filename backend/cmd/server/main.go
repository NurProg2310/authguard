package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/authguard/backend/internal/cache"
	"github.com/authguard/backend/internal/config"
	"github.com/authguard/backend/internal/database"
	"github.com/authguard/backend/internal/httpapi"
	"github.com/authguard/backend/internal/mail"
	"github.com/authguard/backend/internal/security"
	"github.com/authguard/backend/internal/service"
	"github.com/authguard/backend/internal/weather"
)

func main() {
	// Used as the container HEALTHCHECK (the distroless image has no curl).
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool, log); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	rdb, err := cache.Connect(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, log)
	if err != nil {
		return err
	}
	defer rdb.Close()

	auth, err := service.NewAuth(service.Options{
		DB:                 pool,
		Redis:              rdb,
		Tokens:             security.NewTokenManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTAudience, cfg.AccessTTL, cfg.TakeoverTTL),
		Mailer:             mail.New(cfg.SMTP, log),
		Log:                log,
		RefreshTTL:         cfg.RefreshTTL,
		SessionMaxLifetime: cfg.SessionMaxLifetime,
		TakeoverTTL:        cfg.TakeoverTTL,
		ResetTTL:           cfg.ResetTTL,
		AppURL:             cfg.AppURL,
	})
	if err != nil {
		return err
	}
	go auth.RunJanitor(ctx)

	router, err := httpapi.NewRouter(httpapi.Deps{Config: cfg, Log: log, Auth: auth, DB: pool, Redis: rdb, Weather: weather.New(rdb)})
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func healthcheck() int {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/api/health")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
