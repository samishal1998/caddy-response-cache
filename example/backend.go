//go:build ignore

// backend is a simple upstream HTTP server for manually testing the cache plugin.
//
// Run with: go run example/backend.go
// Then: curl -v http://localhost:8080/api/test
package main

import (
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

func main() {
	var counter int64

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&counter, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"request_number":%d,"path":%q,"time":%q}`,
			n, r.URL.Path, time.Now().Format(time.RFC3339))
	})

	log.Println("backend listening on :9000")
	log.Fatal(http.ListenAndServe(":9000", nil))
}
