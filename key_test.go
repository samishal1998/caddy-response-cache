package responsecache

import (
	"net/http"
	"net/url"
	"testing"
)

func TestBuildCacheKey_Default(t *testing.T) {
	r := &http.Request{
		Method: "GET",
		Host:   "example.com",
		URL:    &url.URL{Path: "/api/users", RawQuery: "page=1"},
	}

	key := BuildCacheKey(r, DefaultCacheKeyTemplate)
	expected := "GET_example.com/api/users?page=1"
	if key != expected {
		t.Errorf("got %q, want %q", key, expected)
	}
}

func TestBuildCacheKey_NoQuery(t *testing.T) {
	r := &http.Request{
		Method: "GET",
		Host:   "example.com",
		URL:    &url.URL{Path: "/api/users"},
	}

	key := BuildCacheKey(r, DefaultCacheKeyTemplate)
	expected := "GET_example.com/api/users?"
	if key != expected {
		t.Errorf("got %q, want %q", key, expected)
	}
}

func TestBuildCacheKey_CustomTemplate(t *testing.T) {
	r := &http.Request{
		Method: "POST",
		Host:   "api.example.com:8080",
		URL:    &url.URL{Path: "/v2/data", RawQuery: "id=42"},
	}

	key := BuildCacheKey(r, "{host}{path}")
	expected := "api.example.com:8080/v2/data"
	if key != expected {
		t.Errorf("got %q, want %q", key, expected)
	}
}

func TestBuildCacheKey_WithScheme(t *testing.T) {
	r := &http.Request{
		Method: "GET",
		Host:   "example.com",
		URL:    &url.URL{Path: "/test"},
	}

	key := BuildCacheKey(r, "{scheme}://{host}{path}")
	expected := "http://example.com/test"
	if key != expected {
		t.Errorf("got %q, want %q", key, expected)
	}
}
