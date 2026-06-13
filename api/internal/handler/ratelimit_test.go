package handler

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiter_AllowsUpToBurst(t *testing.T) {
	const burst = 5
	rl := NewRateLimiter(burst, time.Second)
	defer rl.Stop()

	for i := 0; i < burst; i++ {
		if !rl.Allow("user1") {
			t.Fatalf("request %d/%d should be allowed", i+1, burst)
		}
	}
}

func TestRateLimiter_DeniesAfterBurst(t *testing.T) {
	const burst = 3
	rl := NewRateLimiter(burst, time.Second)
	defer rl.Stop()

	for i := 0; i < burst; i++ {
		rl.Allow("user1")
	}
	if rl.Allow("user1") {
		t.Error("request beyond burst limit should be denied")
	}
}

func TestRateLimiter_IndependentPerUser(t *testing.T) {
	const burst = 2
	rl := NewRateLimiter(burst, time.Second)
	defer rl.Stop()

	// Exhaust user1's budget
	rl.Allow("user1")
	rl.Allow("user1")
	if rl.Allow("user1") {
		t.Error("user1 should be rate-limited")
	}

	// user2 should still have a fresh budget
	if !rl.Allow("user2") {
		t.Error("user2 should not be rate-limited")
	}
}

func TestRateLimiter_Stop(t *testing.T) {
	rl := NewRateLimiter(10, time.Second)
	// Should not panic or block
	rl.Stop()
}

func TestRateLimiter_EvictsStaleEntries(t *testing.T) {
	// Use a very short window so entries quickly become stale.
	rl := NewRateLimiter(100, 50*time.Millisecond)
	defer rl.Stop()

	rl.Allow("stale-user")
	if len(rl.limiters) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(rl.limiters))
	}

	// Wait past the window so the entry becomes stale.
	time.Sleep(100 * time.Millisecond)

	// Trigger eviction by adding a new user when at capacity.
	rl.maxUsers = 1 // force eviction on next new user
	rl.Allow("new-user")

	if _, exists := rl.limiters["stale-user"]; exists {
		t.Error("stale entry should have been evicted")
	}
}

func TestRateLimiter_NewUserCreatedOnFirstRequest(t *testing.T) {
	rl := NewRateLimiter(10, time.Second)
	defer rl.Stop()

	if len(rl.limiters) != 0 {
		t.Fatalf("expected empty limiters, got %d", len(rl.limiters))
	}
	rl.Allow("new-user")
	if len(rl.limiters) != 1 {
		t.Errorf("expected 1 limiter after first request, got %d", len(rl.limiters))
	}
}

func TestRateLimiter_LastSeenUpdatedOnSubsequentRequests(t *testing.T) {
	rl := NewRateLimiter(10, time.Second)
	defer rl.Stop()

	rl.Allow("user1")
	before := rl.limiters["user1"].lastSeen

	time.Sleep(5 * time.Millisecond)
	rl.Allow("user1")
	after := rl.limiters["user1"].lastSeen

	if !after.After(before) {
		t.Error("lastSeen should be updated on subsequent requests")
	}
}

func TestRateLimiter_CleanupRemovesExpiredOnTick(t *testing.T) {
ctx, cancel := context.WithCancel(context.Background())
rl := &RateLimiter{
limiters:        make(map[string]*limiterEntry),
rps:             100,
burst:           100,
window:          10 * time.Millisecond,
maxUsers:        1000,
stop:            cancel,
cleanupInterval: 20 * time.Millisecond,
}

rl.Allow("user1")
if len(rl.limiters) != 1 {
t.Fatalf("expected 1 entry after Allow, got %d", len(rl.limiters))
}

// Wait past the window so the entry becomes stale
time.Sleep(50 * time.Millisecond)

done := make(chan struct{})
go func() {
defer close(done)
rl.cleanup(ctx)
}()

// Give cleanup goroutine time to tick at least once
time.Sleep(60 * time.Millisecond)
cancel()
<-done

rl.mu.Lock()
_, exists := rl.limiters["user1"]
rl.mu.Unlock()

if exists {
t.Error("stale entry should have been evicted by cleanup ticker")
}
}
