.PHONY: build run test clean

CADDY_VERSION ?= v2.8.4

build:
	xcaddy build $(CADDY_VERSION) --with github.com/samishal1998/caddy-response-cache=.

run: build
	./caddy run --config example/Caddyfile

test:
	go test -v -race ./...

clean:
	rm -f caddy
