package storage

import (
	"context"
	"time"
)

// Storage is the interface for L2 cache backends.
// All data is stored as raw bytes; serialization is handled by the caller.
type Storage interface {
	// Get retrieves a cached entry by key. Returns nil, false if not found.
	Get(ctx context.Context, key string) ([]byte, bool)

	// Set stores data with a TTL.
	Set(ctx context.Context, key string, data []byte, ttl time.Duration) error

	// Delete removes a cached entry by key.
	Delete(ctx context.Context, key string) error

	// Purge removes all cached entries.
	Purge(ctx context.Context) error

	// Close releases resources held by the storage backend.
	Close() error
}
