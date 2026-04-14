package responsecache

import (
	"net/http"
	"testing"
	"time"
)

func TestEncodeDecode_RoundTrip(t *testing.T) {
	entry := &CacheEntry{
		StatusCode: 200,
		Header: http.Header{
			"Content-Type":  {"application/json"},
			"X-Request-Id":  {"abc-123"},
			"Cache-Control": {"public, max-age=300"},
		},
		Body:      []byte(`{"message":"hello world"}`),
		CreatedAt: time.Now().Truncate(time.Second),
		ExpiresAt: time.Now().Add(5 * time.Minute).Truncate(time.Second),
	}

	data, err := EncodeCacheEntry(entry)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeCacheEntry(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded == nil {
		t.Fatal("decoded entry is nil (expired?)")
	}

	if decoded.StatusCode != entry.StatusCode {
		t.Errorf("status code: got %d, want %d", decoded.StatusCode, entry.StatusCode)
	}
	if string(decoded.Body) != string(entry.Body) {
		t.Errorf("body: got %q, want %q", decoded.Body, entry.Body)
	}
	if decoded.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type: got %q", decoded.Header.Get("Content-Type"))
	}
	if decoded.Header.Get("X-Request-Id") != "abc-123" {
		t.Errorf("X-Request-Id: got %q", decoded.Header.Get("X-Request-Id"))
	}
}

func TestEncodeDecode_EmptyBody(t *testing.T) {
	entry := &CacheEntry{
		StatusCode: 204,
		Header:     http.Header{},
		Body:       nil,
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(time.Minute),
	}

	data, err := EncodeCacheEntry(entry)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeCacheEntry(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded == nil {
		t.Fatal("decoded entry is nil")
	}
	if decoded.StatusCode != 204 {
		t.Errorf("status code: got %d, want 204", decoded.StatusCode)
	}
	if len(decoded.Body) != 0 {
		t.Errorf("body should be empty, got %d bytes", len(decoded.Body))
	}
}

func TestDecode_Expired(t *testing.T) {
	entry := &CacheEntry{
		StatusCode: 200,
		Header:     http.Header{},
		Body:       []byte("old data"),
		CreatedAt:  time.Now().Add(-10 * time.Minute),
		ExpiresAt:  time.Now().Add(-5 * time.Minute),
	}

	data, err := EncodeCacheEntry(entry)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeCacheEntry(data)
	if err != nil {
		t.Fatalf("decode should not error for expired: %v", err)
	}
	if decoded != nil {
		t.Error("decoded should be nil for expired entry")
	}
}

func TestDecode_InvalidData(t *testing.T) {
	_, err := DecodeCacheEntry([]byte("not valid gob"))
	if err == nil {
		t.Error("decode should fail for invalid data")
	}
}

func TestEncodeDecode_LargeBody(t *testing.T) {
	body := make([]byte, 1<<20) // 1 MB
	for i := range body {
		body[i] = byte(i % 256)
	}

	entry := &CacheEntry{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/octet-stream"}},
		Body:       body,
		CreatedAt:  time.Now(),
		ExpiresAt:  time.Now().Add(time.Hour),
	}

	data, err := EncodeCacheEntry(entry)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := DecodeCacheEntry(data)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if decoded == nil {
		t.Fatal("decoded is nil")
	}
	if len(decoded.Body) != len(body) {
		t.Errorf("body length: got %d, want %d", len(decoded.Body), len(body))
	}
}
