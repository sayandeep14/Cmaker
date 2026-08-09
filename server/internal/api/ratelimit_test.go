package api

import (
	"testing"
	"time"
)

func TestPublishRateLimiterAllowsUpToMax(t *testing.T) {
	l := newPublishRateLimiter()
	for i := range publishRateLimitMax {
		if !l.Allow(1) {
			t.Fatalf("Allow() denied attempt %d, want allowed (max is %d)", i+1, publishRateLimitMax)
		}
	}
}

func TestPublishRateLimiterDeniesOverMax(t *testing.T) {
	l := newPublishRateLimiter()
	for range publishRateLimitMax {
		l.Allow(1)
	}
	if l.Allow(1) {
		t.Error("Allow() on the attempt after the max: expected denial, got allowed")
	}
}

func TestPublishRateLimiterTracksUsersIndependently(t *testing.T) {
	l := newPublishRateLimiter()
	for range publishRateLimitMax {
		l.Allow(1)
	}
	if !l.Allow(2) {
		t.Error("Allow() for a different user should not be affected by user 1's limit")
	}
}

func TestPublishRateLimiterPrunesOldAttempts(t *testing.T) {
	l := newPublishRateLimiter()
	// Seed publishRateLimitMax attempts that are already outside the
	// window - Allow should prune them and still allow this call.
	old := time.Now().Add(-publishRateLimitWindow - time.Minute)
	seeded := make([]time.Time, publishRateLimitMax)
	for i := range seeded {
		seeded[i] = old
	}
	l.attempts[1] = seeded

	if !l.Allow(1) {
		t.Error("Allow() with only stale (out-of-window) attempts on record: expected allowed, got denied")
	}
	if len(l.attempts[1]) != 1 {
		t.Errorf("attempts[1] = %d entries after pruning, want 1 (just the new one)", len(l.attempts[1]))
	}
}
