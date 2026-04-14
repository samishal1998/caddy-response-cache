package responsecache

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	c, err := NewCache(&MemoryConfig{MaxItems: 100}, nil, 5*time.Minute)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func testEntry(ttl time.Duration) *CacheEntry {
	return &CacheEntry{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"text/plain"}},
		Body:       []byte("hello"),
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(ttl),
	}
}

func TestCache_SetAndGet(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	entry := testEntry(5 * time.Minute)
	if err := c.Set(ctx, "key1", entry); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// otter is eventually consistent — give it a moment
	time.Sleep(50 * time.Millisecond)

	got, status := c.Get(ctx, "key1")
	if status != CacheHit {
		t.Fatalf("expected Hit, got %s", status)
	}
	if got.StatusCode != 200 {
		t.Errorf("status code: got %d, want 200", got.StatusCode)
	}
	if string(got.Body) != "hello" {
		t.Errorf("body: got %q, want %q", got.Body, "hello")
	}
}

func TestCache_Miss(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	_, status := c.Get(ctx, "nonexistent")
	if status != CacheMiss {
		t.Errorf("expected Miss, got %s", status)
	}
}

func TestCache_Delete(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	c.Set(ctx, "key1", testEntry(5*time.Minute))
	time.Sleep(50 * time.Millisecond)

	if err := c.Delete(ctx, "key1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, status := c.Get(ctx, "key1")
	if status != CacheMiss {
		t.Errorf("expected Miss after delete, got %s", status)
	}
}

func TestCache_Purge(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	c.Set(ctx, "a", testEntry(5*time.Minute))
	c.Set(ctx, "b", testEntry(5*time.Minute))
	time.Sleep(50 * time.Millisecond)

	if err := c.Purge(ctx); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	_, s1 := c.Get(ctx, "a")
	_, s2 := c.Get(ctx, "b")
	if s1 != CacheMiss || s2 != CacheMiss {
		t.Errorf("expected all misses after purge, got %s, %s", s1, s2)
	}
}

func TestCache_ExpiredEntry(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	// Create an entry that is already expired
	entry := &CacheEntry{
		StatusCode: 200,
		Header:     http.Header{},
		Body:       []byte("old"),
		CreatedAt:  time.Now().Add(-10 * time.Minute),
		ExpiresAt:  time.Now().Add(-1 * time.Second),
	}
	c.Set(ctx, "expired", entry)
	time.Sleep(50 * time.Millisecond)

	_, status := c.Get(ctx, "expired")
	if status != CacheMiss {
		t.Errorf("expected Miss for expired entry, got %s", status)
	}
}
