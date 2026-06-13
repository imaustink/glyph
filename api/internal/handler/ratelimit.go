package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glyph/api/internal/auth"
	"golang.org/x/time/rate"
)

// RateLimiter implements a per-user rate limiter using token buckets.
// Memory is bounded: entries are evicted after inactivity exceeds the window.
type RateLimiter struct {
	mu              sync.Mutex
	limiters        map[string]*limiterEntry
	rps             rate.Limit
	burst           int
	window          time.Duration
	maxUsers        int
	stop            context.CancelFunc
	cleanupInterval time.Duration
}

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter creates a rate limiter allowing rps requests/sec with the given burst.
// maxUsers caps the number of tracked users to bound memory usage.
// Call Stop() during graceful shutdown to release the background cleanup goroutine.
func NewRateLimiter(maxRequests int, window time.Duration) *RateLimiter {
	rps := rate.Limit(float64(maxRequests) / window.Seconds())
	ctx, cancel := context.WithCancel(context.Background())
	rl := &RateLimiter{
		limiters:        make(map[string]*limiterEntry),
		rps:             rps,
		burst:           maxRequests,
		window:          window,
		maxUsers:        10000,
		stop:            cancel,
		cleanupInterval: time.Minute,
	}
	go rl.cleanup(ctx)
	return rl
}

// Stop terminates the background cleanup goroutine. Call during server shutdown.
func (rl *RateLimiter) Stop() {
	rl.stop()
}

// Allow checks if a request from the given user should be allowed.
func (rl *RateLimiter) Allow(userID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry, ok := rl.limiters[userID]
	if !ok {
		// Evict oldest entries if at capacity
		if len(rl.limiters) >= rl.maxUsers {
			rl.evictOldest()
		}
		entry = &limiterEntry{
			limiter:  rate.NewLimiter(rl.rps, rl.burst),
			lastSeen: time.Now(),
		}
		rl.limiters[userID] = entry
	} else {
		entry.lastSeen = time.Now()
	}

	return entry.limiter.Allow()
}

// evictOldest removes entries that haven't been seen within the window.
// Must be called with rl.mu held.
func (rl *RateLimiter) evictOldest() {
	cutoff := time.Now().Add(-rl.window)
	for id, entry := range rl.limiters {
		if entry.lastSeen.Before(cutoff) {
			delete(rl.limiters, id)
		}
	}
}

// cleanup periodically removes stale entries to prevent unbounded growth.
func (rl *RateLimiter) cleanup(ctx context.Context) {
	ticker := time.NewTicker(rl.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-rl.window)
			for id, entry := range rl.limiters {
				if entry.lastSeen.Before(cutoff) {
					delete(rl.limiters, id)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// RateLimitMiddleware returns a Gin middleware that rate-limits requests per user.
func RateLimitMiddleware(rl *RateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		user := auth.CurrentUser(c)
		if user == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		if !rl.Allow(user.ID.String()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded, please try again later",
			})
			return
		}

		c.Next()
	}
}
