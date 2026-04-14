.PHONY: build run test clean

build:
	xcaddy build --with github.com/samimishal/response-cache=.

run: build
	./caddy run --config example/Caddyfile

test:
	go test -v -race ./...

clean:
	rm -f caddy
