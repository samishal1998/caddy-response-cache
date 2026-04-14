package responsecache

import (
	"context"
	"time"

	"github.com/maypok86/otter/v2"
	"github.com/samimishal/response-cache/storage"
)

// CacheStatus indicates the result of a cache lookup.
type CacheStatus string

const (
	CacheHit    CacheStatus = "Hit"
	CacheMiss   CacheStatus = "Miss"
	CacheBypass CacheStatus = "Bypass"
)

// Cache is a two-layer cache orchestrator.
// L1 is an in-memory otter cache storing deserialized *CacheEntry.
// L2 is an optional storage backend (Redis or file) storing serialized bytes.
type Cache struct {
	l1  *otter.Cache[string, *CacheEntry]
	l2  storage.Storage
	ttl time.Duration
}

// MemoryConfig holds configuration for the in-memory L1 cache.
type MemoryConfig struct {
	MaxSize  int64 `json:"max_size,omitempty"`
	MaxItems int   `json:"max_items,omitempty"`
}

// NewCache creates a new two-layer cache.
// l2 may be nil for memory-only caching.
func NewCache(memoryCfg *MemoryConfig, l2 storage.Storage, ttl time.Duration) (*Cache, error) {
	opts := &otter.Options[string, *CacheEntry]{
		ExpiryCalculator: otter.ExpiryCreating[string, *CacheEntry](ttl),
	}

	if memoryCfg.MaxSize > 0 {
		opts.MaximumWeight = uint64(memoryCfg.MaxSize)
		opts.Weigher = func(key string, value *CacheEntry) uint32 {
			weight := len(key) + len(value.Body) + 512
			if weight > int(^uint32(0)) {
				return ^uint32(0)
			}
			return uint32(weight)
		}
	} else {
		maxItems := memoryCfg.MaxItems
		if maxItems <= 0 {
			maxItems = 10000
		}
		opts.MaximumSize = maxItems
	}

	l1, err := otter.New(opts)
	if err != nil {
		return nil, err
	}

	return &Cache{l1: l1, l2: l2, ttl: ttl}, nil
}

// Get looks up a cache entry. It checks L1 first, then L2.
// On an L2 hit the entry is promoted to L1.
func (c *Cache) Get(ctx context.Context, key string) (*CacheEntry, CacheStatus) {
	// L1 lookup
	if entry, ok := c.l1.GetIfPresent(key); ok {
		if time.Now().Before(entry.ExpiresAt) {
			return entry, CacheHit
		}
		c.l1.Invalidate(key)
	}

	// L2 lookup
	if c.l2 == nil {
		return nil, CacheMiss
	}

	data, ok := c.l2.Get(ctx, key)
	if !ok {
		return nil, CacheMiss
	}

	entry, err := DecodeCacheEntry(data)
	if err != nil {
		c.l2.Delete(ctx, key)
		return nil, CacheMiss
	}
	if entry == nil {
		// expired
		c.l2.Delete(ctx, key)
		return nil, CacheMiss
	}

	// Promote to L1
	c.l1.Set(key, entry)

	return entry, CacheHit
}

// Set writes a cache entry to both L1 and L2.
func (c *Cache) Set(ctx context.Context, key string, entry *CacheEntry) error {
	c.l1.Set(key, entry)

	if c.l2 == nil {
		return nil
	}

	data, err := EncodeCacheEntry(entry)
	if err != nil {
		return err
	}
	return c.l2.Set(ctx, key, data, time.Until(entry.ExpiresAt))
}

// Delete removes an entry from both L1 and L2.
func (c *Cache) Delete(ctx context.Context, key string) error {
	c.l1.Invalidate(key)
	if c.l2 != nil {
		return c.l2.Delete(ctx, key)
	}
	return nil
}

// Purge removes all entries from both L1 and L2.
func (c *Cache) Purge(ctx context.Context) error {
	c.l1.InvalidateAll()
	if c.l2 != nil {
		return c.l2.Purge(ctx)
	}
	return nil
}

// Close releases resources for both layers.
func (c *Cache) Close() error {
	c.l1.InvalidateAll()
	if c.l2 != nil {
		return c.l2.Close()
	}
	return nil
}
