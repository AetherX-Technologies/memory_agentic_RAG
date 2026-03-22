// Package ratelimit implements token-bucket rate limiting for the memory system.
// Default limits: 20 ops/minute, 200 ops/hour to prevent memory bloat.
package ratelimit

import (
	"fmt"
	"sync"
	"time"
)

// Limiter implements a sliding-window rate limiter.
type Limiter struct {
	mu          sync.Mutex
	perMinute   int
	perHour     int
	minuteSlots []time.Time
	hourSlots   []time.Time
}

// Config holds rate limit configuration.
type Config struct {
	PerMinute int // max operations per minute (default: 20)
	PerHour   int // max operations per hour (default: 200)
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{PerMinute: 20, PerHour: 200}
}

// New creates a new rate limiter.
func New(cfg Config) *Limiter {
	if cfg.PerMinute <= 0 {
		cfg.PerMinute = 20
	}
	if cfg.PerHour <= 0 {
		cfg.PerHour = 200
	}
	return &Limiter{
		perMinute:   cfg.PerMinute,
		perHour:     cfg.PerHour,
		minuteSlots: make([]time.Time, 0, cfg.PerMinute),
		hourSlots:   make([]time.Time, 0, cfg.PerHour),
	}
}

// Allow checks if an operation is allowed under the rate limit.
// Returns nil if allowed, or an error with retry-after info if rate limited.
func (l *Limiter) Allow() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	// Clean expired slots
	l.minuteSlots = pruneOlderThan(l.minuteSlots, now.Add(-time.Minute))
	l.hourSlots = pruneOlderThan(l.hourSlots, now.Add(-time.Hour))

	// Check minute limit
	if len(l.minuteSlots) >= l.perMinute {
		oldest := l.minuteSlots[0]
		retryAfter := oldest.Add(time.Minute).Sub(now)
		return &RateLimitError{
			RetryAfter: retryAfter,
			Message:    fmt.Sprintf("rate limit exceeded: %d/%d per minute", len(l.minuteSlots), l.perMinute),
		}
	}

	// Check hour limit
	if len(l.hourSlots) >= l.perHour {
		oldest := l.hourSlots[0]
		retryAfter := oldest.Add(time.Hour).Sub(now)
		return &RateLimitError{
			RetryAfter: retryAfter,
			Message:    fmt.Sprintf("rate limit exceeded: %d/%d per hour", len(l.hourSlots), l.perHour),
		}
	}

	// Record this operation
	l.minuteSlots = append(l.minuteSlots, now)
	l.hourSlots = append(l.hourSlots, now)
	return nil
}

// RateLimitError indicates the operation was rate limited.
type RateLimitError struct {
	RetryAfter time.Duration
	Message    string
}

func (e *RateLimitError) Error() string {
	return e.Message
}

// pruneOlderThan removes timestamps older than cutoff.
func pruneOlderThan(slots []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(slots) && !slots[i].After(cutoff) {
		i++
	}
	return slots[i:]
}
