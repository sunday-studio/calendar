// Command fakegoogle is a stand-alone fake Google (OAuth + Calendar v3) for end-to-end runs of the
// real hub with no credentials. Admin controls: POST /_admin/{fail,reset,seed,etag-bump,revoke,
// unrevoke,expire-sync-tokens}, GET /_admin/state. See docs/engineering/e2e.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"calendar/server/internal/provider/google/gtest"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8788", "listen address")
	accts := flag.String("accounts", "a@example.com,b@example.com", "comma-separated account emails (each seeded with its own calendars and events)")
	flag.Parse()
	m := gtest.NewMulti(strings.Split(*accts, ",")...)
	srv := &http.Server{Addr: *addr, Handler: m.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sc, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = srv.Shutdown(sc)
	}()
	fmt.Fprintf(os.Stderr, "fakegoogle listening on %s (accounts %s)\n", *addr, *accts)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
