package auth

import (
	"context"
	"sync"
	"time"
)

type bucket struct {
	tokens float64
	last   time.Time
}

// Limiter is a keyed token-bucket rate limiter.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
	now     func() time.Time
	lastGC  time.Time
}

// NewLimiter allows `perMinute` events per minute per key with the given burst.
func NewLimiter(perMinute float64, burst int) *Limiter {
	return &Limiter{buckets: map[string]*bucket{}, rate: perMinute / 60, burst: float64(burst), now: time.Now}
}

// Allow reports whether one event is permitted for key.
func (l *Limiter) Allow(key string) bool {
	return l.AllowN(key, 1)
}

// AllowN reports whether n events are permitted for key.
func (l *Limiter) AllowN(key string, n float64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.lastGC) > 5*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.last) > 10*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < n {
		return false
	}
	b.tokens -= n
	return true
}

// Semaphore bounds concurrency.
type Semaphore chan struct{}

// NewSemaphore creates a semaphore with n slots.
func NewSemaphore(n int) Semaphore { return make(Semaphore, n) }

// TryAcquire takes a slot without blocking.
func (s Semaphore) TryAcquire() bool {
	select {
	case s <- struct{}{}:
		return true
	default:
		return false
	}
}

// Acquire blocks until a slot is available.
func (s Semaphore) Acquire() { s <- struct{}{} }

// AcquireCtx waits for a slot until ctx is done; false means it gave up.
func (s Semaphore) AcquireCtx(ctx context.Context) bool {
	select {
	case s <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// Release frees a slot.
func (s Semaphore) Release() { <-s }

// KeyedSemaphore bounds concurrency per key.
type KeyedSemaphore struct {
	mu    sync.Mutex
	limit int
	count map[string]int
	wake  chan struct{} // fechado e trocado a cada Release: acorda quem espera em Acquire
}

// NewKeyedSemaphore creates a per-key limiter.
func NewKeyedSemaphore(limit int) *KeyedSemaphore {
	return &KeyedSemaphore{limit: limit, count: map[string]int{}, wake: make(chan struct{})}
}

// Acquire waits for a slot for key until ctx is done; false means it gave up.
func (k *KeyedSemaphore) Acquire(ctx context.Context, key string) bool {
	for {
		k.mu.Lock()
		if k.count[key] < k.limit {
			k.count[key]++
			k.mu.Unlock()
			return true
		}
		wake := k.wake
		k.mu.Unlock()
		select {
		case <-wake:
		case <-ctx.Done():
			return false
		}
	}
}

// TryAcquire takes a slot for key if under the limit.
func (k *KeyedSemaphore) TryAcquire(key string) bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.count[key] >= k.limit {
		return false
	}
	k.count[key]++
	return true
}

// Release frees a slot for key.
func (k *KeyedSemaphore) Release(key string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.count[key] <= 1 {
		delete(k.count, key)
	} else {
		k.count[key]--
	}
	close(k.wake)
	k.wake = make(chan struct{})
}
