package storage

import (
	"context"
	"os"
	"testing"
	"time"
)

func newTestFileStorage(t *testing.T) *FileStorage {
	t.Helper()
	dir, err := os.MkdirTemp("", "cache-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	fs, err := NewFileStorage(dir)
	if err != nil {
		t.Fatalf("NewFileStorage: %v", err)
	}
	return fs
}

func TestFileStorage_SetAndGet(t *testing.T) {
	fs := newTestFileStorage(t)
	ctx := context.Background()

	data := []byte("cached response body")
	if err := fs.Set(ctx, "test-key", data, 5*time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, ok := fs.Get(ctx, "test-key")
	if !ok {
		t.Fatal("expected to find key")
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}
}

func TestFileStorage_GetMiss(t *testing.T) {
	fs := newTestFileStorage(t)
	ctx := context.Background()

	_, ok := fs.Get(ctx, "nonexistent")
	if ok {
		t.Error("expected miss for nonexistent key")
	}
}

func TestFileStorage_Delete(t *testing.T) {
	fs := newTestFileStorage(t)
	ctx := context.Background()

	fs.Set(ctx, "key1", []byte("data"), 5*time.Minute)

	if err := fs.Delete(ctx, "key1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, ok := fs.Get(ctx, "key1")
	if ok {
		t.Error("expected miss after delete")
	}
}

func TestFileStorage_DeleteNonexistent(t *testing.T) {
	fs := newTestFileStorage(t)
	ctx := context.Background()

	if err := fs.Delete(ctx, "nonexistent"); err != nil {
		t.Errorf("Delete nonexistent should not error: %v", err)
	}
}

func TestFileStorage_Purge(t *testing.T) {
	fs := newTestFileStorage(t)
	ctx := context.Background()

	fs.Set(ctx, "a", []byte("1"), 5*time.Minute)
	fs.Set(ctx, "b", []byte("2"), 5*time.Minute)

	if err := fs.Purge(ctx); err != nil {
		t.Fatalf("Purge: %v", err)
	}

	_, ok1 := fs.Get(ctx, "a")
	_, ok2 := fs.Get(ctx, "b")
	if ok1 || ok2 {
		t.Error("expected all misses after purge")
	}

	// Ensure we can still write after purge
	if err := fs.Set(ctx, "c", []byte("3"), 5*time.Minute); err != nil {
		t.Fatalf("Set after purge: %v", err)
	}
}

func TestFileStorage_Overwrite(t *testing.T) {
	fs := newTestFileStorage(t)
	ctx := context.Background()

	fs.Set(ctx, "key", []byte("v1"), 5*time.Minute)
	fs.Set(ctx, "key", []byte("v2"), 5*time.Minute)

	got, ok := fs.Get(ctx, "key")
	if !ok {
		t.Fatal("expected to find key")
	}
	if string(got) != "v2" {
		t.Errorf("got %q, want %q", got, "v2")
	}
}
