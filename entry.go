package responsecache

import (
	"bytes"
	"encoding/gob"
	"net/http"
	"time"
)

// CacheEntry holds all data needed to reconstruct a cached HTTP response.
type CacheEntry struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// EncodeCacheEntry serializes a CacheEntry to bytes using gob encoding.
func EncodeCacheEntry(e *CacheEntry) ([]byte, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(e); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeCacheEntry deserializes bytes into a CacheEntry using gob decoding.
// Returns nil if the entry has expired.
func DecodeCacheEntry(data []byte) (*CacheEntry, error) {
	var e CacheEntry
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&e); err != nil {
		return nil, err
	}
	if time.Now().After(e.ExpiresAt) {
		return nil, nil
	}
	return &e, nil
}
