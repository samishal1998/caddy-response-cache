//go:build ignore

// backend-status is an upstream server that returns configurable status codes
// based on the URL path. It is used to manually test status_ttl behavior.
//
// Run with: go run example/backend-status.go
//
//	GET /ok        -> 200
//	GET /missing   -> 404
//	GET /broken    -> 500
//	GET /redirect  -> 301
package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
)

func main() {
	var counter int64

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&counter, 1)
		w.Header().Set("Content-Type", "text/plain")

		var status int
		switch {
		case strings.Contains(r.URL.Path, "missing"):
			status = 404
		case strings.Contains(r.URL.Path, "broken"):
			status = 500
		case strings.Contains(r.URL.Path, "redirect"):
			status = 301
			w.Header().Set("Location", "/elsewhere")
		default:
			status = 200
		}

		w.WriteHeader(status)
		fmt.Fprintf(w, "%d at %s (request #%d)\n", status, r.URL.Path, n)
	})

	log.Println("status backend listening on :9000")
	log.Fatal(http.ListenAndServe(":9000", nil))
}
