package update

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// CacheFile is the file under the groot home that remembers the last check.
const CacheFile = "update-check.json"

// CheckEvery is the least time between two calls to the release API.
const CheckEvery = 24 * time.Hour

// Cache is what a check leaves behind. Latest is the last version seen; Error
// is the reason the last call failed, empty when it worked.
type Cache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// Options configures Check. A nil Client uses http.DefaultClient and a nil Now
// uses time.Now.
type Options struct {
	Client *http.Client
	Repo   string
	Token  string
	Now    func() time.Time
}

// Result is the outcome of Check: the latest version known and whether it is
// newer than the running build.
type Result struct {
	Latest string
	Newer  bool
}

// Status is what the cache alone says, for doctor. Reason explains why the
// latest version is unknown when Latest is empty.
type Status struct {
	Latest string
	Newer  bool
	Reason string
}

// Check returns the latest release, asking GitHub at most once per CheckEvery
// and remembering the answer in home. Errors are never returned: an offline
// machine simply sees no newer release. A check cancelled by ctx is not
// recorded, so the next start tries again.
func Check(ctx context.Context, home, current string, opt Options) Result {
	now := opt.now()
	cache, _ := readCache(home)
	if age := now.Sub(cache.CheckedAt); !cache.CheckedAt.IsZero() && age >= 0 && age < CheckEvery {
		return result(current, cache.Latest)
	}

	client := opt.Client
	if client == nil {
		client = http.DefaultClient
	}
	rel, err := Latest(ctx, client, opt.Repo, opt.Token)
	if ctx.Err() != nil {
		return result(current, cache.Latest)
	}
	next := Cache{CheckedAt: now, Latest: cache.Latest}
	if err != nil {
		next.Error = err.Error()
	} else {
		next.Latest = rel.Version
	}
	_ = writeCache(home, next)
	return result(current, next.Latest)
}

// Known reads the cache without any network call.
func Known(home, current string) Status {
	cache, err := readCache(home)
	switch {
	case cache.Latest != "":
		return Status{Latest: cache.Latest, Newer: Newer(current, cache.Latest)}
	case cache.Error != "":
		return Status{Reason: cache.Error}
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return Status{Reason: "cache unreadable: " + err.Error()}
	}
	return Status{Reason: "not checked yet"}
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

func result(current, latest string) Result {
	return Result{Latest: latest, Newer: Newer(current, latest)}
}

func readCache(home string) (Cache, error) {
	var c Cache
	raw, err := os.ReadFile(filepath.Join(home, CacheFile))
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return Cache{}, err
	}
	return c, nil
}

func writeCache(home string, c Cache) error {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, CacheFile), raw, 0o600)
}
