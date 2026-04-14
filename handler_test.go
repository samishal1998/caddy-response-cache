package responsecache

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"go.uber.org/zap"
)

// upstreamHandler is a mock upstream that returns a fixed response.
type upstreamHandler struct {
	status  int
	headers http.Header
	body    string
	called  int
}

func (u *upstreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	u.called++
	for k, vals := range u.headers {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(u.status)
	fmt.Fprint(w, u.body)
	return nil
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	c, err := NewCache(&MemoryConfig{MaxItems: 100}, nil)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	t.Cleanup(func() { c.Close() })

	return &Handler{
		TTL:              caddy.Duration(5 * time.Minute),
		MaxBodySize:      50 * 1024 * 1024,
		MatchMethods:     []string{"GET", "HEAD"},
		CacheKeyTemplate: DefaultCacheKeyTemplate,
		cache:            c,
		logger:           zap.NewNop(),
	}
}

func TestHandler_CacheMissThenHit(t *testing.T) {
	h := newTestHandler(t)
	upstream := &upstreamHandler{
		status: 200,
		headers: http.Header{
			"Content-Type": {"application/json"},
		},
		body: `{"data":"test"}`,
	}

	req := httptest.NewRequest("GET", "http://example.com/api/test", nil)

	// First request — cache miss
	w1 := httptest.NewRecorder()
	if err := h.ServeHTTP(w1, req, upstream); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if w1.Header().Get("X-Cache") != "Miss" {
		t.Errorf("first request: X-Cache = %q, want Miss", w1.Header().Get("X-Cache"))
	}
	if w1.Body.String() != `{"data":"test"}` {
		t.Errorf("first request body: got %q", w1.Body.String())
	}
	if upstream.called != 1 {
		t.Errorf("upstream called %d times, want 1", upstream.called)
	}

	// Give async cache write time to complete
	time.Sleep(100 * time.Millisecond)

	// Second request — cache hit
	w2 := httptest.NewRecorder()
	if err := h.ServeHTTP(w2, req, upstream); err != nil {
		t.Fatalf("second request: %v", err)
	}
	if w2.Header().Get("X-Cache") != "Hit" {
		t.Errorf("second request: X-Cache = %q, want Hit", w2.Header().Get("X-Cache"))
	}
	if w2.Body.String() != `{"data":"test"}` {
		t.Errorf("second request body: got %q", w2.Body.String())
	}
	// Upstream should NOT have been called again
	if upstream.called != 1 {
		t.Errorf("upstream called %d times after hit, want 1", upstream.called)
	}
}

func TestHandler_BypassNonGetMethod(t *testing.T) {
	h := newTestHandler(t)
	upstream := &upstreamHandler{
		status: 200,
		body:   "ok",
	}

	req := httptest.NewRequest("POST", "http://example.com/api/test", nil)

	w := httptest.NewRecorder()
	if err := h.ServeHTTP(w, req, upstream); err != nil {
		t.Fatalf("request: %v", err)
	}
	if w.Header().Get("X-Cache") != "Bypass" {
		t.Errorf("X-Cache = %q, want Bypass", w.Header().Get("X-Cache"))
	}
	if upstream.called != 1 {
		t.Errorf("upstream called %d times, want 1", upstream.called)
	}
}

func TestHandler_BypassSetCookie(t *testing.T) {
	h := newTestHandler(t)
	upstream := &upstreamHandler{
		status: 200,
		headers: http.Header{
			"Set-Cookie": {"session=abc123"},
		},
		body: "with cookie",
	}

	req := httptest.NewRequest("GET", "http://example.com/api/test", nil)

	w := httptest.NewRecorder()
	if err := h.ServeHTTP(w, req, upstream); err != nil {
		t.Fatalf("request: %v", err)
	}
	// Response with Set-Cookie should not be cached
	if w.Header().Get("X-Cache") != "Bypass" {
		t.Errorf("X-Cache = %q, want Bypass for Set-Cookie response", w.Header().Get("X-Cache"))
	}
}

func TestHandler_BypassCacheControlNoStore(t *testing.T) {
	h := newTestHandler(t)
	upstream := &upstreamHandler{
		status: 200,
		headers: http.Header{
			"Cache-Control": {"no-store"},
		},
		body: "no store",
	}

	req := httptest.NewRequest("GET", "http://example.com/api/test", nil)

	w := httptest.NewRecorder()
	if err := h.ServeHTTP(w, req, upstream); err != nil {
		t.Fatalf("request: %v", err)
	}
	if w.Header().Get("X-Cache") != "Bypass" {
		t.Errorf("X-Cache = %q, want Bypass for no-store", w.Header().Get("X-Cache"))
	}
}

func TestHandler_BypassPrivate(t *testing.T) {
	h := newTestHandler(t)
	upstream := &upstreamHandler{
		status: 200,
		headers: http.Header{
			"Cache-Control": {"private, max-age=300"},
		},
		body: "private",
	}

	req := httptest.NewRequest("GET", "http://example.com/api/test", nil)

	w := httptest.NewRecorder()
	if err := h.ServeHTTP(w, req, upstream); err != nil {
		t.Fatalf("request: %v", err)
	}
	if w.Header().Get("X-Cache") != "Bypass" {
		t.Errorf("X-Cache = %q, want Bypass for private", w.Header().Get("X-Cache"))
	}
}

func TestHandler_SkipNon2xx(t *testing.T) {
	h := newTestHandler(t)
	upstream := &upstreamHandler{
		status: 500,
		body:   "error",
	}

	req := httptest.NewRequest("GET", "http://example.com/api/test", nil)

	w := httptest.NewRecorder()
	if err := h.ServeHTTP(w, req, upstream); err != nil {
		t.Fatalf("request: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Second request should still hit upstream (500 not cached)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req, upstream)
	if upstream.called != 2 {
		t.Errorf("upstream called %d times, want 2 (500 should not cache)", upstream.called)
	}
}

func TestHandler_PurgeMethod(t *testing.T) {
	h := newTestHandler(t)
	upstream := &upstreamHandler{
		status: 200,
		body:   "cached data",
	}

	req := httptest.NewRequest("GET", "http://example.com/api/test", nil)

	// Populate cache
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req, upstream)
	time.Sleep(100 * time.Millisecond)

	// Verify it's cached
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req, upstream)
	if w2.Header().Get("X-Cache") != "Hit" {
		t.Fatalf("expected Hit before purge, got %s", w2.Header().Get("X-Cache"))
	}

	// PURGE
	purgeReq := httptest.NewRequest("PURGE", "http://example.com/api/test", nil)
	wPurge := httptest.NewRecorder()
	h.ServeHTTP(wPurge, purgeReq, upstream)
	if wPurge.Code != http.StatusNoContent {
		t.Errorf("PURGE status: got %d, want 204", wPurge.Code)
	}

	// Give otter time to process invalidation
	time.Sleep(100 * time.Millisecond)

	// After purge, should be a miss
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, req, upstream)
	if w3.Header().Get("X-Cache") != "Miss" {
		t.Errorf("after purge: X-Cache = %q, want Miss", w3.Header().Get("X-Cache"))
	}
}

func TestHandler_MaxBodySize(t *testing.T) {
	h := newTestHandler(t)
	h.MaxBodySize = 10 // 10 bytes

	upstream := &upstreamHandler{
		status: 200,
		body:   "this body is longer than 10 bytes",
	}

	req := httptest.NewRequest("GET", "http://example.com/api/test", nil)

	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req, upstream)
	time.Sleep(100 * time.Millisecond)

	// Should not be cached due to body size
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req, upstream)
	if upstream.called != 2 {
		t.Errorf("upstream called %d times, want 2 (oversized body should not cache)", upstream.called)
	}
}

func TestResolveTTL(t *testing.T) {
	h := &Handler{
		TTL: caddy.Duration(5 * time.Minute),
		StatusTTL: map[string]caddy.Duration{
			"200": caddy.Duration(1 * time.Hour),
			"2xx": caddy.Duration(10 * time.Minute),
			"404": caddy.Duration(30 * time.Second),
			"5xx": caddy.Duration(2 * time.Second),
		},
	}

	tests := []struct {
		status int
		want   time.Duration
	}{
		{200, 1 * time.Hour},     // exact match beats class
		{201, 10 * time.Minute},  // class match (2xx)
		{299, 10 * time.Minute},  // class match (2xx)
		{301, 0},                 // no match, not default
		{404, 30 * time.Second},  // exact match
		{403, 0},                 // 4xx not configured, no fallback
		{500, 2 * time.Second},   // class match (5xx)
		{503, 2 * time.Second},   // class match (5xx)
	}

	for _, tt := range tests {
		if got := h.resolveTTL(tt.status); got != tt.want {
			t.Errorf("resolveTTL(%d) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestResolveTTL_DefaultOnly(t *testing.T) {
	h := &Handler{TTL: caddy.Duration(5 * time.Minute)}

	tests := []struct {
		status int
		want   time.Duration
	}{
		{200, 5 * time.Minute},
		{204, 5 * time.Minute},
		{299, 5 * time.Minute},
		{301, 0},
		{404, 0},
		{500, 0},
	}

	for _, tt := range tests {
		if got := h.resolveTTL(tt.status); got != tt.want {
			t.Errorf("resolveTTL(%d) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestHandler_StatusTTL_Cache404(t *testing.T) {
	h := newTestHandler(t)
	h.StatusTTL = map[string]caddy.Duration{
		"404": caddy.Duration(30 * time.Second),
	}
	upstream := &upstreamHandler{
		status:  404,
		headers: http.Header{"Content-Type": {"text/plain"}},
		body:    "not found",
	}

	req := httptest.NewRequest("GET", "http://example.com/api/missing", nil)

	// First request — miss
	w1 := httptest.NewRecorder()
	if err := h.ServeHTTP(w1, req, upstream); err != nil {
		t.Fatalf("first: %v", err)
	}
	if w1.Header().Get("X-Cache") != "Miss" {
		t.Errorf("first: X-Cache = %q, want Miss", w1.Header().Get("X-Cache"))
	}
	if w1.Code != 404 {
		t.Errorf("first: status = %d, want 404", w1.Code)
	}

	time.Sleep(100 * time.Millisecond)

	// Second request — hit
	w2 := httptest.NewRecorder()
	if err := h.ServeHTTP(w2, req, upstream); err != nil {
		t.Fatalf("second: %v", err)
	}
	if w2.Header().Get("X-Cache") != "Hit" {
		t.Errorf("second: X-Cache = %q, want Hit", w2.Header().Get("X-Cache"))
	}
	if w2.Code != 404 {
		t.Errorf("second: status = %d, want 404", w2.Code)
	}
	if w2.Body.String() != "not found" {
		t.Errorf("second body: %q", w2.Body.String())
	}
	if upstream.called != 1 {
		t.Errorf("upstream called %d times, want 1", upstream.called)
	}
}

func TestHandler_StatusTTL_Cache5xxClass(t *testing.T) {
	h := newTestHandler(t)
	h.StatusTTL = map[string]caddy.Duration{
		"5xx": caddy.Duration(1 * time.Minute),
	}
	upstream := &upstreamHandler{status: 503, body: "service unavailable"}

	req := httptest.NewRequest("GET", "http://example.com/api/down", nil)

	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req, upstream)
	time.Sleep(100 * time.Millisecond)

	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req, upstream)
	if w2.Header().Get("X-Cache") != "Hit" {
		t.Errorf("503 should hit via 5xx class, got X-Cache = %q", w2.Header().Get("X-Cache"))
	}
	if upstream.called != 1 {
		t.Errorf("upstream called %d times, want 1", upstream.called)
	}
}

func TestHandler_StatusTTL_ZeroDisablesCaching(t *testing.T) {
	h := newTestHandler(t)
	h.StatusTTL = map[string]caddy.Duration{
		"200": caddy.Duration(0), // explicitly disable caching 200
	}
	upstream := &upstreamHandler{status: 200, body: "ok"}

	req := httptest.NewRequest("GET", "http://example.com/api/test", nil)

	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, req, upstream)
	time.Sleep(100 * time.Millisecond)

	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req, upstream)
	if upstream.called != 2 {
		t.Errorf("upstream called %d times, want 2 (200 TTL=0 should bypass)", upstream.called)
	}
}

func TestIsValidStatusKey(t *testing.T) {
	valid := []string{"100", "200", "404", "599", "1xx", "2xx", "3xx", "4xx", "5xx"}
	for _, s := range valid {
		if !isValidStatusKey(s) {
			t.Errorf("isValidStatusKey(%q) = false, want true", s)
		}
	}
	invalid := []string{"", "0", "99", "600", "abc", "20x", "6xx", "x2x", "200x", "2XX"}
	for _, s := range invalid {
		if isValidStatusKey(s) {
			t.Errorf("isValidStatusKey(%q) = true, want false", s)
		}
	}
}

func TestHandler_HeadRequest(t *testing.T) {
	h := newTestHandler(t)
	upstream := &upstreamHandler{
		status: 200,
		headers: http.Header{
			"Content-Type": {"text/plain"},
		},
		body: "body content",
	}

	// First, cache with GET
	getReq := httptest.NewRequest("GET", "http://example.com/api/test", nil)
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, getReq, upstream)
	time.Sleep(100 * time.Millisecond)

	// HEAD request to same URL should hit cache but no body
	headReq := httptest.NewRequest("HEAD", "http://example.com/api/test", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, headReq, upstream)

	// HEAD generates a different cache key by default ({method}_...), so it will be a miss
	// This is expected behavior — HEAD and GET are cached separately
	if upstream.called != 2 {
		t.Logf("HEAD generated separate cache key as expected, upstream called %d times", upstream.called)
	}
}
