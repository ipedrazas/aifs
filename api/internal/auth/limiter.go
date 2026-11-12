package auth

import (
	"math"
	"sync"
	"time"
)

// limiter is a token bucket per key (client IP) plus one global bucket, so
// neither a single client nor many clients together can brute-force the
// password quickly.
type limiter struct {
	mu      sync.Mutex
	perKey  bucketSpec
	global  bucketSpec
	buckets map[string]*bucket
	all     bucket
	now     func() time.Time
}

type bucketSpec struct {
	burst float64
	every time.Duration // one token is refilled every interval
}

type bucket struct {
	tokens float64
	last   time.Time
}

const maxTrackedKeys = 10_000

func newLimiter(perKey, global bucketSpec, now func() time.Time) *limiter {
	return &limiter{
		perKey:  perKey,
		global:  global,
		buckets: map[string]*bucket{},
		all:     bucket{tokens: global.burst, last: now()},
		now:     now,
	}
}

// allow takes one token from the key's bucket and the global bucket. When
// either is empty it returns false and how long until a token is available.
func (l *limiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= maxTrackedKeys {
			l.evictFull(now)
		}
		b = &bucket{tokens: l.perKey.burst, last: now}
		l.buckets[key] = b
	}
	b.refill(l.perKey, now)
	l.all.refill(l.global, now)

	if b.tokens < 1 {
		return false, b.wait(l.perKey)
	}
	if l.all.tokens < 1 {
		return false, l.all.wait(l.global)
	}
	b.tokens--
	l.all.tokens--
	return true, 0
}

// evictFull drops buckets that have refilled completely: forgetting them
// loses no state.
func (l *limiter) evictFull(now time.Time) {
	for k, b := range l.buckets {
		b.refill(l.perKey, now)
		if b.tokens >= l.perKey.burst {
			delete(l.buckets, k)
		}
	}
}

func (b *bucket) refill(s bucketSpec, now time.Time) {
	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.tokens = math.Min(s.burst, b.tokens+float64(elapsed)/float64(s.every))
		b.last = now
	}
}

func (b *bucket) wait(s bucketSpec) time.Duration {
	return time.Duration((1-b.tokens)*float64(s.every)).Round(time.Second) + time.Second
}
