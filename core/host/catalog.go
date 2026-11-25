package host

import (
	"context"
	"log"
	"time"

	"github.com/ebur-dev/ebur/core/catalog"
)

// Catalog refresh timing (SEC-02): a first check soon after start (not at
// once, so it never slows a start), then daily.
var (
	catalogFirst = 2 * time.Minute
	catalogEvery = 24 * time.Hour
)

// refreshCatalog fetches the signed runtime manifest daily. Failures are
// logged once per run of failures and never affect services: Ebur keeps
// the manifest it has.
func (h *Host) refreshCatalog(ctx context.Context) {
	wait := catalogFirst
	lastErr := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = catalogEvery
		rctx, cancel := context.WithTimeout(ctx, time.Minute)
		res, err := catalog.Refresh(rctx, h.Root, nil)
		cancel()
		switch {
		case err != nil:
			if err.Error() != lastErr {
				log.Printf("runtime list: could not check for a newer one: %v", err)
				lastErr = err.Error()
			}
		case res.Updated:
			lastErr = ""
			log.Printf("runtime list: %s", res.Message)
		default:
			lastErr = ""
		}
	}
}
