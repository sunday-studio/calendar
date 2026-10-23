// Command fakegoogle is a stand-alone fake Google (OAuth + Calendar v3) for end-to-end runs of the
// real hub with no credentials. Admin controls: POST /_admin/{fail,reset,seed,etag-bump,revoke,
// unrevoke,expire-sync-tokens} (docs/engineering/e2e.md) and, for the demo, {down,rate-limit,slow,
// recover,reauth,outside,burst,calendar-add,calendar-remove} plus GET /_admin/{state,summary}
// (docs/ops/demo-data.md).
//
// --profile e2e (default) is the small dataset the e2e scripts assert on; --profile rich is the
// demo dataset with every UI state (4 accounts, relative dates).
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
	profile := flag.String("profile", gtest.ProfileE2E, "dataset: e2e (small, asserted by the e2e scripts) or rich (demo, every UI state)")
	accts := flag.String("accounts", "", "comma-separated account emails (default: a@example.com,b@example.com for e2e; the four demo accounts for rich)")
	tz := flag.String("tz", "", "rich profile: time zone whose midnight starts \"today\" (default Europe/London)")
	flag.Parse()
	if *profile != gtest.ProfileE2E && *profile != gtest.ProfileRich {
		log.Fatalf("unknown --profile %q (e2e|rich)", *profile)
	}
	if *accts == "" {
		*accts = "a@example.com,b@example.com"
		if *profile == gtest.ProfileRich {
			*accts = strings.Join(gtest.RichAccounts(), ",")
		}
	}
	m := gtest.NewMultiProfile(*profile, strings.Split(*accts, ",")...)
	m.TZ = *tz
	m.Reset()
	srv := &http.Server{Addr: *addr, Handler: m.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		sc, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		_ = srv.Shutdown(sc)
	}()
	fmt.Fprintf(os.Stderr, "fakegoogle listening on %s (profile %s, accounts %s)\n", *addr, *profile, *accts)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
