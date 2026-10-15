// Package cache owns the optional Redis client and its bounded operations.
package cache

import (
	"context"
	"errors"
	"time"

	"github.com/kanatovs/taskflow-api/internal/config"
	"github.com/redis/go-redis/v9"
)

type Redis struct {
	client  *redis.Client
	timeout time.Duration
}

// New creates a lazy client. Redis availability never gates application startup.
func New(cfg config.Redis) *Redis {
	return &Redis{client: redis.NewClient(&redis.Options{
		Addr: cfg.Addr, Password: cfg.Password, DB: cfg.DB,
		DialTimeout: cfg.DialTimeout, ReadTimeout: cfg.OperationTimeout,
		WriteTimeout: cfg.OperationTimeout, PoolTimeout: cfg.OperationTimeout,
		ContextTimeoutEnabled: true, MaxRetries: -1, DialerRetries: 1,
	}), timeout: cfg.OperationTimeout}
}

func (r *Redis) Get(ctx context.Context, key string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	value, err := r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	return value, err == nil, err
}

func (r *Redis) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *Redis) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.client.Del(ctx, key).Err()
}

func (r *Redis) Close() error { return r.client.Close() }
