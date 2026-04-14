.PHONY: build run test clean

build:
	xcaddy build --with github.com/samishal1998/caddy-response-cache=.

run: build
	./caddy run --config example/Caddyfile

test:
	go test -v -race ./...

clean:
	rm -f caddy
