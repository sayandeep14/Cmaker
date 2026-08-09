package api

import (
	"sync"
	"time"
)

// publishRateLimitWindow/Max bound how many publish attempts (POST
// /v1/packs/{name}/versions - CreateVersion, not the later /complete
// step) one user can make - a single basic guard against an accidental
// or runaway publish loop, not a serious abuse-prevention system (see
// PACKS_PLAN.md §10's explicit "general API rate limiting" non-goal).
const (
	publishRateLimitWindow = time.Hour
	publishRateLimitMax    = 20
)

// publishRateLimiter is an in-memory per-user sliding-window limiter -
// fine for a single-instance POC (see fly.toml's one shared-cpu-1x
// machine); would need a shared store (Redis, the Postgres database
// itself) if this server ever ran more than one instance, since each
// instance would otherwise track its own independent counts.
type publishRateLimiter struct {
	mu       sync.Mutex
	attempts map[int64][]time.Time
}

func newPublishRateLimiter() *publishRateLimiter {
	return &publishRateLimiter{attempts: map[int64][]time.Time{}}
}

// Allow records one attempt for userID and reports whether it's within
// the limit - prunes attempts older than the window on every call, so
// memory use stays bounded to active users' recent activity, not every
// user who's ever published.
func (l *publishRateLimiter) Allow(userID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := time.Now().Add(-publishRateLimitWindow)
	kept := l.attempts[userID][:0]
	for _, t := range l.attempts[userID] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= publishRateLimitMax {
		l.attempts[userID] = kept
		return false
	}
	l.attempts[userID] = append(kept, time.Now())
	return true
}
