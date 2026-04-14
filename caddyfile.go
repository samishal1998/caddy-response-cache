package responsecache

import (
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/dustin/go-humanize"
)

func init() {
	httpcaddyfile.RegisterHandlerDirective("response_cache", parseCaddyfile)
	httpcaddyfile.RegisterDirectiveOrder("response_cache", httpcaddyfile.Before, "reverse_proxy")
}

// parseCaddyfile sets up the handler from Caddyfile tokens.
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var handler Handler
	err := handler.UnmarshalCaddyfile(h.Dispenser)
	return &handler, err
}

// UnmarshalCaddyfile parses the Caddyfile configuration for the response_cache directive.
//
//	response_cache {
//	    ttl 5m
//	    max_body_size 50MB
//	    match_path /api/*
//	    match_methods GET HEAD
//	    cache_key {method}_{host}{path}?{query}
//
//	    memory {
//	        max_size 256MB
//	        max_items 10000
//	    }
//
//	    redis {
//	        addr localhost:6379
//	        password ""
//	        db 0
//	        key_prefix cache:
//	    }
//
//	    file {
//	        path /tmp/caddy-cache
//	    }
//	}
func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next() // consume directive name "response_cache"

	for d.NextBlock(0) {
		switch d.Val() {
		case "ttl":
			if !d.NextArg() {
				return d.ArgErr()
			}
			dur, err := caddy.ParseDuration(d.Val())
			if err != nil {
				return d.Errf("invalid ttl duration: %v", err)
			}
			h.TTL = caddy.Duration(dur)

		case "max_body_size":
			if !d.NextArg() {
				return d.ArgErr()
			}
			size, err := humanize.ParseBytes(d.Val())
			if err != nil {
				return d.Errf("invalid max_body_size: %v", err)
			}
			h.MaxBodySize = int64(size)

		case "match_path":
			args := d.RemainingArgs()
			if len(args) == 0 {
				return d.ArgErr()
			}
			h.MatchPath = append(h.MatchPath, args...)

		case "match_methods":
			args := d.RemainingArgs()
			if len(args) == 0 {
				return d.ArgErr()
			}
			h.MatchMethods = args

		case "cache_key":
			if !d.NextArg() {
				return d.ArgErr()
			}
			h.CacheKeyTemplate = d.Val()

		case "memory":
			h.Memory = &MemoryConfig{}
			for d.NextBlock(1) {
				switch d.Val() {
				case "max_size":
					if !d.NextArg() {
						return d.ArgErr()
					}
					size, err := humanize.ParseBytes(d.Val())
					if err != nil {
						return d.Errf("invalid memory max_size: %v", err)
					}
					h.Memory.MaxSize = int64(size)
				case "max_items":
					if !d.NextArg() {
						return d.ArgErr()
					}
					n, err := strconv.Atoi(d.Val())
					if err != nil {
						return d.Errf("invalid memory max_items: %v", err)
					}
					h.Memory.MaxItems = n
				default:
					return d.Errf("unrecognized memory subdirective: %s", d.Val())
				}
			}

		case "redis":
			h.Redis = &RedisConfig{}
			for d.NextBlock(1) {
				switch d.Val() {
				case "addr":
					if !d.NextArg() {
						return d.ArgErr()
					}
					h.Redis.Addr = d.Val()
				case "password":
					if !d.NextArg() {
						return d.ArgErr()
					}
					h.Redis.Password = d.Val()
				case "db":
					if !d.NextArg() {
						return d.ArgErr()
					}
					n, err := strconv.Atoi(d.Val())
					if err != nil {
						return d.Errf("invalid redis db: %v", err)
					}
					h.Redis.DB = n
				case "key_prefix":
					if !d.NextArg() {
						return d.ArgErr()
					}
					h.Redis.KeyPrefix = d.Val()
				default:
					return d.Errf("unrecognized redis subdirective: %s", d.Val())
				}
			}

		case "file":
			h.File = &FileConfig{}
			for d.NextBlock(1) {
				switch d.Val() {
				case "path":
					if !d.NextArg() {
						return d.ArgErr()
					}
					h.File.Path = d.Val()
				default:
					return d.Errf("unrecognized file subdirective: %s", d.Val())
				}
			}

		default:
			return d.Errf("unrecognized response_cache subdirective: %s", d.Val())
		}
	}

	return nil
}
