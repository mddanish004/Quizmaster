package ratelimit

import (
	"sync"
	"time"
)

// Bucket is a simple token bucket.
type Bucket struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
	rate   float64 // tokens per second
	burst  float64
}

// Limiter holds named buckets keyed by string.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*Bucket
	rate    float64
	burst   float64
	enabled bool
}

// New creates a limiter with rate tokens/sec and burst capacity.
func New(rate, burst float64, enabled bool) *Limiter {
	return &Limiter{buckets: map[string]*Bucket{}, rate: rate, burst: burst, enabled: enabled}
}

// Allow reports whether one token may be consumed for key.
func (l *Limiter) Allow(key string) bool {
	if !l.enabled {
		return true
	}
	l.mu.Lock()
	b, ok := l.buckets[key]
	if !ok {
		b = &Bucket{tokens: l.burst, last: time.Now(), rate: l.rate, burst: l.burst}
		l.buckets[key] = b
	}
	l.mu.Unlock()
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
