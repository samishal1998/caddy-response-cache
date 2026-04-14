# response-cache

A [Caddy 2](https://caddyserver.com/) HTTP middleware plugin that caches reverse proxy responses using a two-layer cache:

- **L1**: in-memory cache ([otter v2](https://github.com/maypok86/otter), W-TinyLFU eviction)
- **L2**: optional persistent backend — Redis or local filesystem

It caches the full response (status code, headers, body), respects basic `Cache-Control` directives, supports `PURGE` requests, and is intentionally focused on the reverse-proxy use case. It is not RFC 7234 compliant — if you need full HTTP caching semantics, use [caddyserver/cache-handler](https://github.com/caddyserver/cache-handler) instead.

## Features

- Two-layer caching: fast in-memory L1 with a persistent L2 fallback (Redis or file)
- Automatic L1 promotion on L2 hits
- Configurable TTL, max body size, path/method matchers, and cache key template
- `X-Cache: Hit|Miss|Bypass` response header
- `PURGE` HTTP method to invalidate a single cached entry
- Skips caching for `Set-Cookie`, `Cache-Control: no-store|no-cache|private`, non-2xx responses, and oversized bodies
- Async cache writes so responses are never blocked on storage I/O

## Requirements

- Go 1.25 or newer
- Caddy 2.5+ (tested against v2.11.2)
- [xcaddy](https://github.com/caddyserver/xcaddy) for building
- Optional: a running Redis server for the Redis L2 backend

## Building

Install `xcaddy` if you don't already have it:

```sh
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
```

### Build from the local checkout

Clone the repo and build a Caddy binary that includes the plugin:

```sh
git clone https://github.com/samimishal/response-cache.git
cd response-cache
make build
```

The `Makefile` wraps:

```sh
xcaddy build --with github.com/samimishal/response-cache=.
```

This produces a `./caddy` binary in the project directory.

### Build against a remote version

If you want to include the plugin in an existing Caddy build from any directory:

```sh
xcaddy build --with github.com/samimishal/response-cache@latest
```

### Verify the module is registered

```sh
./caddy list-modules | grep response_cache
# http.handlers.response_cache
```

## Quick start

1. Start a backend to proxy to (the repo ships a tiny demo backend):

   ```sh
   go run example/backend.go
   ```

   This listens on `:9000` and returns an incrementing JSON payload so you can tell cached and fresh responses apart.

2. Run Caddy with the example config:

   ```sh
   ./caddy run --config example/Caddyfile
   ```

3. Hit the endpoint twice and watch the `X-Cache` header:

   ```sh
   curl -i http://localhost:8080/api/test
   # HTTP/1.1 200 OK
   # X-Cache: Miss
   # {"request_number":1, ...}

   curl -i http://localhost:8080/api/test
   # HTTP/1.1 200 OK
   # X-Cache: Hit
   # {"request_number":1, ...}   <-- same response, no upstream call
   ```

4. Purge and re-fetch:

   ```sh
   curl -i -X PURGE http://localhost:8080/api/test
   # HTTP/1.1 204 No Content

   curl -i http://localhost:8080/api/test
   # X-Cache: Miss
   # {"request_number":2, ...}   <-- upstream was called again
   ```

## Caddyfile directive reference

The plugin registers a `response_cache` directive that is ordered before `reverse_proxy` by default.

```caddyfile
response_cache {
    ttl            <duration>          # default: 5m
    max_body_size  <size>              # default: 50MB
    match_path     <pattern> [<pattern>...]
    match_methods  <method> [<method>...]   # default: GET HEAD
    cache_key      <template>          # default: {method}_{host}{path}?{query}

    memory {
        max_items  <n>                 # L1 entry count limit
        max_size   <size>              # L1 byte limit (overrides max_items)
    }

    # Pick at most one L2 backend:
    redis {
        addr        <host:port>
        password    <password>
        db          <n>
        key_prefix  <prefix>           # default: cache:
    }

    file {
        path  <directory>
    }
}
```

### Top-level options

| Option | Description | Default |
|---|---|---|
| `ttl` | How long cached responses remain valid. Accepts Go duration syntax (`5m`, `1h`, `30s`, `24h`). | `5m` |
| `max_body_size` | Responses larger than this are passed through without caching. Accepts humanized sizes (`50MB`, `1GB`, `512KB`). | `50MB` |
| `match_path` | Glob patterns to limit which paths are cached. If omitted, all paths are candidates. | *(all paths)* |
| `match_methods` | HTTP methods to cache. | `GET HEAD` |
| `cache_key` | Template for generating cache keys. Placeholders: `{method}`, `{host}`, `{path}`, `{query}`, `{scheme}`. | `{method}_{host}{path}?{query}` |

### `memory` block (L1, always enabled)

| Option | Description | Default |
|---|---|---|
| `max_items` | Maximum number of entries to hold in memory. Uses W-TinyLFU eviction. | `10000` |
| `max_size` | Maximum total byte weight. If set, overrides `max_items` and switches otter into weight-based eviction. | *(unset)* |

### `redis` block (L2 option)

| Option | Description | Default |
|---|---|---|
| `addr` | Redis server address. | *(required)* |
| `password` | Auth password. | `""` |
| `db` | Database index. | `0` |
| `key_prefix` | Namespace prepended to every key stored in Redis. | `cache:` |

Redis TTLs are enforced server-side using `SET … EX`, so expired entries are cleaned up automatically. `Purge` uses `SCAN` + `DEL` limited to `key_prefix*` — it does **not** issue `FLUSHDB`.

### `file` block (L2 option)

| Option | Description | Default |
|---|---|---|
| `path` | Directory where cached entries are written. Created on startup if missing. | *(required)* |

Entries are written atomically: a temp file is created then renamed. Keys are hashed with SHA-256 and distributed across 256 subdirectories (`path/{2-char-prefix}/{remaining-hex}`) to avoid overloading a single directory.

File TTLs are enforced at read time — the orchestrator decodes each entry and discards expired ones. There is currently **no background sweeper**, so expired files remain on disk until they are next requested. If long-term disk growth is a concern, use Redis or add a cron job to clean the directory.

### Request skip rules

A response is **not** cached if any of the following is true:

- Request method is not in `match_methods`
- Request path does not match any `match_path` pattern (when `match_path` is set)
- Status code is outside `200–299`
- Response has a `Set-Cookie` header
- Response `Cache-Control` contains `no-store`, `no-cache`, or `private`
- Response body exceeds `max_body_size`

When a request is skipped, the response still includes `X-Cache: Bypass` so you can tell from the client side.

## Example configurations

### Memory-only (no L2)

Great for single-node setups where you don't need cached data to survive restarts.

```caddyfile
:8080 {
    response_cache {
        ttl 10m
        memory {
            max_items 5000
        }
    }
    reverse_proxy localhost:9000
}
```

### Memory + Redis L2

Use Redis when you want cached entries to outlive restarts or be shared across multiple Caddy instances.

```caddyfile
:8080 {
    response_cache {
        ttl 1h
        max_body_size 100MB

        memory {
            max_items 10000
        }

        redis {
            addr       redis.internal:6379
            password   {env.REDIS_PASSWORD}
            db         0
            key_prefix mysite:
        }
    }

    reverse_proxy api.internal:8080
}
```

### Memory + file L2

Handy for single-node setups that still want persistence across restarts without running Redis.

```caddyfile
:8080 {
    response_cache {
        ttl 30m

        memory {
            max_items 10000
        }

        file {
            path /var/cache/caddy
        }
    }

    reverse_proxy localhost:9000
}
```

### Path and method scoping

Only cache the public JSON API, leaving everything else alone.

```caddyfile
example.com {
    response_cache {
        ttl 5m
        match_path    /api/public/*
        match_methods GET

        memory {
            max_items 20000
        }
    }

    reverse_proxy app:3000
}
```

### Custom cache key with scheme

If you serve the same origin over both HTTP and HTTPS and want separate cache entries per scheme:

```caddyfile
:80, :443 {
    response_cache {
        ttl 5m
        cache_key {scheme}_{method}_{host}{path}?{query}
        memory { max_items 10000 }
    }
    reverse_proxy backend:8080
}
```

### Short TTL for stale-tolerant endpoints

Micro-caching is useful for shielding slow upstreams from traffic spikes:

```caddyfile
:8080 {
    response_cache {
        ttl 2s
        match_path /expensive/*
        memory { max_items 1000 }
    }
    reverse_proxy slow-upstream:8080
}
```

### JSON config

If you prefer Caddy's native JSON config, the handler module is `http.handlers.response_cache`:

```json
{
    "handler": "response_cache",
    "ttl": 300000000000,
    "max_body_size": 52428800,
    "match_methods": ["GET", "HEAD"],
    "memory": { "max_items": 10000 },
    "file":   { "path": "/var/cache/caddy" }
}
```

Durations in JSON are in nanoseconds (`300000000000` = 5 minutes).

## Invalidation

Send a `PURGE` request to the same URL you want to evict:

```sh
curl -X PURGE http://localhost:8080/api/test
```

The handler rewrites the key as if the request were a `GET`, so `PURGE /api/test` will correctly delete whatever `GET /api/test` cached.

There is currently no bulk purge or purge-by-pattern — if you need to drop everything, restart Caddy (for memory-only), delete the cache directory (for file L2), or flush the Redis key prefix (for Redis L2).

## Operational notes

- **Thundering herd**: the plugin does **not** use singleflight. If N concurrent requests arrive for the same uncached URL, all N hit the upstream. If that is a concern, put a short TTL in front of very expensive endpoints and accept the initial burst.
- **Compression**: place `response_cache` before `encode` in the Caddyfile if you are using Caddy's built-in compression. The cache will store uncompressed responses and let `encode` handle per-request compression negotiation.
- **Async cache writes**: response writes to the client happen before the cache is updated. The cache-set goroutine uses `context.Background()`, so it survives request cancellation.

## Development

Run the test suite:

```sh
go test -race -count=1 ./...
```

Tests are pure-Go and do not require Redis. A Redis backend test suite can be added later behind a `//go:build integration` tag.

Repo layout:

```
.
├── entry.go / entry_test.go      # CacheEntry + gob serialization
├── key.go / key_test.go          # Cache key builder
├── cache.go / cache_test.go      # Two-layer orchestrator (otter L1 + Storage L2)
├── handler.go / handler_test.go  # Caddy module + ServeHTTP middleware
├── caddyfile.go                  # Caddyfile parser
├── storage/
│   ├── storage.go                # Storage interface
│   ├── redis.go                  # Redis L2 backend
│   └── file.go / file_test.go    # Filesystem L2 backend
├── example/
│   ├── Caddyfile                 # Memory-only example
│   ├── Caddyfile-file            # Memory + file L2 example
│   └── backend.go                # Demo upstream server
└── Makefile
```

## License

MIT
