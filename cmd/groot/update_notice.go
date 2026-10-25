package main

import (
	"context"
	"time"

	"github.com/tinchoram/groot-cli/internal/update"
)

// updateCheckBudget bounds the startup release check. Startup never waits for it.
const updateCheckBudget = 3 * time.Second

// startUpdateNotice runs the cached release check in the background when
// updates.check is on. The returned channel gets the newer version once, when
// there is one, and is closed either way. It is nil when the check is off.
func startUpdateNotice(enabled bool, home, current, token string) <-chan string {
	if !enabled {
		return nil
	}
	ch := make(chan string, 1)
	go func() {
		defer close(ch)
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckBudget)
		defer cancel()
		res := update.Check(ctx, home, current, update.Options{
			Client: update.NewClient(current),
			Repo:   update.Repo,
			Token:  token,
		})
		if res.Newer {
			ch <- res.Latest
		}
	}()
	return ch
}
