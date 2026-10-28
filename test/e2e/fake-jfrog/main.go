// Command fake-jfrog serves the in-memory fake JFrog Platform used by the
// end-to-end tests. It accepts the access token "fake-token".
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/Bermos/jfrog-operator/internal/jfrog/fake"
)

func main() {
	addr := flag.String("addr", ":8080", "Address to listen on.")
	flag.Parse()
	srv := &http.Server{Addr: *addr, Handler: fake.New().Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("fake JFrog Platform listening on %s", *addr)
	log.Fatal(srv.ListenAndServe())
}
