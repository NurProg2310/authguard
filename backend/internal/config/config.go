// Package config loads runtime configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type SMTP struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

type Config struct {
	Env  string
	Port string

	DatabaseURL string

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	JWTSecret   []byte
	JWTIssuer   string
	JWTAudience string

	AccessTTL          time.Duration
	RefreshTTL         time.Duration
	SessionMaxLifetime time.Duration
	TakeoverTTL        time.Duration
	ResetTTL           time.Duration

	CookieSecure bool
	CookieDomain string

	AppURL         string
	TrustedProxies []string

	SMTP SMTP
}

func (c *Config) IsProduction() bool { return c.Env == "production" }

func Load() (*Config, error) {
	c := &Config{
		Env:           get("APP_ENV", "production"),
		Port:          get("PORT", "8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		RedisAddr:     get("REDIS_ADDR", "redis:6379"),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		JWTSecret:     []byte(os.Getenv("JWT_SECRET")),
		JWTIssuer:     get("JWT_ISSUER", "authguard"),
		JWTAudience:   get("JWT_AUDIENCE", "authguard-web"),
		CookieDomain:  os.Getenv("COOKIE_DOMAIN"),
		AppURL:        strings.TrimRight(get("APP_URL", "http://localhost:8080"), "/"),
		TrustedProxies: splitList(get("TRUSTED_PROXIES",
			"10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.1/32")),
		SMTP: SMTP{
			Host:     os.Getenv("SMTP_HOST"),
			Username: os.Getenv("SMTP_USERNAME"),
			Password: os.Getenv("SMTP_PASSWORD"),
			From:     get("SMTP_FROM", "AuthGuard <no-reply@authguard.local>"),
		},
	}

	var err error
	if c.RedisDB, err = intVal("REDIS_DB", 0); err != nil {
		return nil, err
	}
	if c.SMTP.Port, err = intVal("SMTP_PORT", 587); err != nil {
		return nil, err
	}
	if c.CookieSecure, err = boolVal("COOKIE_SECURE", true); err != nil {
		return nil, err
	}
	durations := []struct {
		dst *time.Duration
		key string
		def time.Duration
	}{
		{&c.AccessTTL, "ACCESS_TOKEN_TTL", 15 * time.Minute},
		{&c.RefreshTTL, "REFRESH_TOKEN_TTL", 7 * 24 * time.Hour},
		{&c.SessionMaxLifetime, "SESSION_MAX_LIFETIME", 30 * 24 * time.Hour},
		{&c.TakeoverTTL, "TAKEOVER_TOKEN_TTL", 5 * time.Minute},
		{&c.ResetTTL, "PASSWORD_RESET_TTL", 30 * time.Minute},
	}
	for _, d := range durations {
		if *d.dst, err = durVal(d.key, d.def); err != nil {
			return nil, err
		}
	}

	if c.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	if len(c.JWTSecret) < 32 {
		return nil, errors.New("JWT_SECRET must be at least 32 bytes")
	}
	if c.AccessTTL > c.RefreshTTL {
		return nil, errors.New("ACCESS_TOKEN_TTL must not exceed REFRESH_TOKEN_TTL")
	}
	u, err := url.Parse(c.AppURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, fmt.Errorf("APP_URL must be an absolute http(s) URL, got %q", c.AppURL)
	}
	if c.IsProduction() && !c.CookieSecure && !isLoopback(u.Hostname()) {
		return nil, errors.New("COOKIE_SECURE must be true in production for non-localhost APP_URL")
	}
	return c, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func get(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func intVal(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func boolVal(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}

func durVal(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return d, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
