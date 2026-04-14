package responsecache

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/samishal1998/caddy-response-cache/storage"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(Handler{})
}

// Handler is a Caddy HTTP middleware that caches reverse proxy responses
// using a two-layer cache: in-memory (L1) and an optional persistent backend (L2).
type Handler struct {
	// TTL is the default cache entry time-to-live for 2xx responses.
	TTL caddy.Duration `json:"ttl,omitempty"`

	// StatusTTL holds per-status-code TTL overrides.
	// Keys can be exact status codes ("200", "404") or classes ("2xx", "5xx").
	// Lookup order: exact code → class → default TTL (for 2xx only).
	// A TTL of 0 disables caching for the matching status.
	StatusTTL map[string]caddy.Duration `json:"status_ttl,omitempty"`

	// MaxBodySize is the maximum response body size (in bytes) to cache.
	MaxBodySize int64 `json:"max_body_size,omitempty"`

	// MatchPath is a list of path patterns to cache. If empty, all paths are cached.
	MatchPath []string `json:"match_path,omitempty"`

	// MatchMethods is the list of HTTP methods to cache. Defaults to GET, HEAD.
	MatchMethods []string `json:"match_methods,omitempty"`

	// CacheKeyTemplate is the template for generating cache keys.
	CacheKeyTemplate string `json:"cache_key,omitempty"`

	// Memory holds the L1 in-memory cache configuration.
	Memory *MemoryConfig `json:"memory,omitempty"`

	// Redis holds the L2 Redis backend configuration.
	Redis *RedisConfig `json:"redis,omitempty"`

	// File holds the L2 file backend configuration.
	File *FileConfig `json:"file,omitempty"`

	cache  *Cache
	logger *zap.Logger
}

// RedisConfig holds Redis L2 backend settings.
type RedisConfig struct {
	Addr      string `json:"addr,omitempty"`
	Password  string `json:"password,omitempty"`
	DB        int    `json:"db,omitempty"`
	KeyPrefix string `json:"key_prefix,omitempty"`
}

// FileConfig holds file L2 backend settings.
type FileConfig struct {
	Path string `json:"path,omitempty"`
}

// CaddyModule returns the Caddy module information.
func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.response_cache",
		New: func() caddy.Module { return new(Handler) },
	}
}

// Provision sets up the handler.
func (h *Handler) Provision(ctx caddy.Context) error {
	h.logger = ctx.Logger()

	// Defaults
	if h.TTL == 0 {
		h.TTL = caddy.Duration(5 * time.Minute)
	}
	if h.MaxBodySize == 0 {
		h.MaxBodySize = 50 * 1024 * 1024 // 50 MB
	}
	if len(h.MatchMethods) == 0 {
		h.MatchMethods = []string{"GET", "HEAD"}
	}
	if h.CacheKeyTemplate == "" {
		h.CacheKeyTemplate = DefaultCacheKeyTemplate
	}
	if h.Memory == nil {
		h.Memory = &MemoryConfig{MaxItems: 10000}
	}

	// Initialize L2
	var l2 storage.Storage
	if h.Redis != nil {
		rs, err := storage.NewRedisStorage(storage.RedisConfig{
			Addr:      h.Redis.Addr,
			Password:  h.Redis.Password,
			DB:        h.Redis.DB,
			KeyPrefix: h.Redis.KeyPrefix,
		})
		if err != nil {
			return fmt.Errorf("initializing redis storage: %w", err)
		}
		l2 = rs
		h.logger.Info("L2 cache backend: Redis", zap.String("addr", h.Redis.Addr))
	} else if h.File != nil {
		fs, err := storage.NewFileStorage(h.File.Path)
		if err != nil {
			return fmt.Errorf("initializing file storage: %w", err)
		}
		l2 = fs
		h.logger.Info("L2 cache backend: File", zap.String("path", h.File.Path))
	}

	cache, err := NewCache(h.Memory, l2, time.Duration(h.TTL))
	if err != nil {
		return fmt.Errorf("initializing cache: %w", err)
	}
	h.cache = cache

	h.logger.Info("cache handler provisioned",
		zap.Duration("ttl", time.Duration(h.TTL)),
		zap.Int64("max_body_size", h.MaxBodySize),
		zap.Strings("methods", h.MatchMethods),
	)
	return nil
}

// Validate ensures the handler configuration is valid.
func (h *Handler) Validate() error {
	if h.Redis != nil && h.File != nil {
		return fmt.Errorf("cannot configure both redis and file as L2 backends")
	}
	if h.TTL < 0 {
		return fmt.Errorf("ttl must be positive")
	}
	if h.MaxBodySize < 0 {
		return fmt.Errorf("max_body_size must be positive")
	}
	for code, ttl := range h.StatusTTL {
		if !isValidStatusKey(code) {
			return fmt.Errorf("invalid status_ttl code %q: must be an exact status (e.g. 200) or class (e.g. 2xx)", code)
		}
		if ttl < 0 {
			return fmt.Errorf("status_ttl for %s must not be negative", code)
		}
	}
	return nil
}

// isValidStatusKey reports whether s is a valid status_ttl key:
// either an exact HTTP status code (100-599) or a class wildcard ("1xx"-"5xx").
func isValidStatusKey(s string) bool {
	if len(s) == 3 && s[1] == 'x' && s[2] == 'x' {
		return s[0] >= '1' && s[0] <= '5'
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	return n >= 100 && n <= 599
}

// resolveTTL returns the effective cache TTL for a response with the given
// status code. It checks the exact status code first, then the class
// (e.g. "2xx"), then falls back to the default TTL for 2xx responses.
// Returns 0 if the response should not be cached.
func (h *Handler) resolveTTL(status int) time.Duration {
	if len(h.StatusTTL) > 0 {
		if ttl, ok := h.StatusTTL[strconv.Itoa(status)]; ok {
			return time.Duration(ttl)
		}
		class := fmt.Sprintf("%dxx", status/100)
		if ttl, ok := h.StatusTTL[class]; ok {
			return time.Duration(ttl)
		}
	}
	// Default: only 2xx uses the top-level TTL.
	if status >= 200 && status < 300 {
		return time.Duration(h.TTL)
	}
	return 0
}

// Cleanup releases resources.
func (h *Handler) Cleanup() error {
	if h.cache != nil {
		return h.cache.Close()
	}
	return nil
}

// bufPool is a pool of byte buffers used for recording upstream responses.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Handle PURGE method — build the key as if it were a GET request
	if r.Method == "PURGE" {
		purgeReq := r.Clone(r.Context())
		purgeReq.Method = "GET"
		key := BuildCacheKey(purgeReq, h.CacheKeyTemplate)
		if err := h.cache.Delete(r.Context(), key); err != nil {
			h.logger.Error("cache purge failed", zap.Error(err))
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}

	// Check if request is cacheable
	if !h.isCacheableRequest(r) {
		w.Header().Set("X-Cache", "Bypass")
		return next.ServeHTTP(w, r)
	}

	key := BuildCacheKey(r, h.CacheKeyTemplate)

	// Cache lookup
	entry, status := h.cache.Get(r.Context(), key)
	if status == CacheHit {
		h.logger.Debug("cache hit", zap.String("key", key))
		return h.serveFromCache(w, r, entry)
	}

	// Cache miss — fetch from upstream
	h.logger.Debug("cache miss", zap.String("key", key))

	// Speculatively mark the response as Bypass. If the response ends up
	// actually cached, we overwrite this to Miss before rec.WriteResponse()
	// is called. Setting the header now ensures it makes it onto the wire
	// even when the response is streamed directly (shouldBuffer=false).
	w.Header().Set("X-Cache", "Bypass")

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		// Don't return oversized buffers to the pool
		if buf.Cap() <= 1<<20 { // 1 MB
			bufPool.Put(buf)
		}
	}()

	rec := caddyhttp.NewResponseRecorder(w, buf, func(status int, header http.Header) bool {
		return h.shouldCacheResponse(status, header)
	})

	err := next.ServeHTTP(rec, r)
	if err != nil {
		return err
	}

	if !rec.Buffered() {
		// Response was streamed directly (shouldCacheResponse returned false).
		// X-Cache: Bypass is already on the wire.
		return nil
	}

	bodyBytes := buf.Bytes()

	// Check body size limit
	if int64(len(bodyBytes)) > h.MaxBodySize {
		// Keep X-Cache: Bypass
		return rec.WriteResponse()
	}

	// Build cache entry with the TTL resolved for this specific status code
	ttl := h.resolveTTL(rec.Status())
	now := time.Now()
	clonedHeader := rec.Header().Clone()
	clonedHeader.Del("X-Cache") // strip our speculative marker
	cacheEntry := &CacheEntry{
		StatusCode: rec.Status(),
		Header:     clonedHeader,
		Body:       make([]byte, len(bodyBytes)),
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
	}
	copy(cacheEntry.Body, bodyBytes)

	// Store in cache asynchronously
	go func() {
		if err := h.cache.Set(context.Background(), key, cacheEntry); err != nil {
			h.logger.Error("cache set failed", zap.String("key", key), zap.Error(err))
		}
	}()

	// Overwrite Bypass → Miss now that we're actually caching
	w.Header().Set("X-Cache", "Miss")
	return rec.WriteResponse()
}

// serveFromCache writes a cached entry to the response writer.
func (h *Handler) serveFromCache(w http.ResponseWriter, r *http.Request, entry *CacheEntry) error {
	for k, vals := range entry.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-Cache", "Hit")
	w.WriteHeader(entry.StatusCode)

	// For HEAD requests, don't write the body
	if r.Method == "HEAD" {
		return nil
	}

	_, err := w.Write(entry.Body)
	return err
}

// isCacheableRequest checks whether the request method and path are eligible for caching.
func (h *Handler) isCacheableRequest(r *http.Request) bool {
	// Check method
	methodAllowed := false
	for _, m := range h.MatchMethods {
		if strings.EqualFold(r.Method, m) {
			methodAllowed = true
			break
		}
	}
	if !methodAllowed {
		return false
	}

	// Check path patterns
	if len(h.MatchPath) > 0 {
		pathMatched := false
		for _, pattern := range h.MatchPath {
			if matched, _ := filepath.Match(pattern, r.URL.Path); matched {
				pathMatched = true
				break
			}
		}
		if !pathMatched {
			return false
		}
	}

	return true
}

// shouldCacheResponse decides whether an upstream response should be cached.
func (h *Handler) shouldCacheResponse(status int, header http.Header) bool {
	// No TTL for this status → don't cache
	if h.resolveTTL(status) <= 0 {
		return false
	}

	// Don't cache responses with Set-Cookie
	if header.Get("Set-Cookie") != "" {
		return false
	}

	// Respect Cache-Control directives
	cc := header.Get("Cache-Control")
	if cc != "" {
		ccLower := strings.ToLower(cc)
		if strings.Contains(ccLower, "no-store") ||
			strings.Contains(ccLower, "no-cache") ||
			strings.Contains(ccLower, "private") {
			return false
		}
	}

	return true
}

// Interface guards
var (
	_ caddy.Module                = (*Handler)(nil)
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddy.Validator             = (*Handler)(nil)
	_ caddy.CleanerUpper          = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
	_ caddyfile.Unmarshaler       = (*Handler)(nil)
)
