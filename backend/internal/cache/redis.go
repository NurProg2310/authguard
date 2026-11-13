// Package cache wires up the Redis client used for session validation and rate limiting.
package cache

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

func Connect(ctx context.Context, addr, password string, db int, log *slog.Logger) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		PoolSize:     20,
	})
	for attempt := 1; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := client.Ping(pingCtx).Err()
		cancel()
		if err == nil {
			return client, nil
		}
		if attempt >= 30 {
			_ = client.Close()
			return nil, fmt.Errorf("redis unreachable: %w", err)
		}
		log.Warn("waiting for redis", "attempt", attempt, "error", err)
		select {
		case <-ctx.Done():
			_ = client.Close()
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
