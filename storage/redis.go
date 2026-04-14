package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Addr      string
	Password  string
	DB        int
	KeyPrefix string
}

// RedisStorage implements Storage using Redis.
type RedisStorage struct {
	client    *redis.Client
	keyPrefix string
}

// NewRedisStorage creates a new Redis storage backend and verifies connectivity.
func NewRedisStorage(cfg RedisConfig) (*RedisStorage, error) {
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = "cache:"
	}
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}
	return &RedisStorage{client: client, keyPrefix: cfg.KeyPrefix}, nil
}

func (r *RedisStorage) Get(ctx context.Context, key string) ([]byte, bool) {
	val, err := r.client.Get(ctx, r.keyPrefix+key).Bytes()
	if err != nil {
		return nil, false
	}
	return val, true
}

func (r *RedisStorage) Set(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	return r.client.Set(ctx, r.keyPrefix+key, data, ttl).Err()
}

func (r *RedisStorage) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, r.keyPrefix+key).Err()
}

func (r *RedisStorage) Purge(ctx context.Context) error {
	iter := r.client.Scan(ctx, 0, r.keyPrefix+"*", 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
		if len(keys) >= 100 {
			r.client.Del(ctx, keys...)
			keys = keys[:0]
		}
	}
	if len(keys) > 0 {
		r.client.Del(ctx, keys...)
	}
	return iter.Err()
}

func (r *RedisStorage) Close() error {
	return r.client.Close()
}
