package responsecache

import (
	"net/http"
	"strings"
)

// DefaultCacheKeyTemplate is the default cache key format.
const DefaultCacheKeyTemplate = "{method}_{host}{path}?{query}"

// BuildCacheKey produces a cache key from the request using the configured template.
func BuildCacheKey(r *http.Request, template string) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}

	replacer := strings.NewReplacer(
		"{method}", r.Method,
		"{host}", r.Host,
		"{path}", r.URL.Path,
		"{query}", r.URL.RawQuery,
		"{scheme}", scheme,
	)
	return replacer.Replace(template)
}
