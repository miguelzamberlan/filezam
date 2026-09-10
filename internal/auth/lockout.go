package auth

import (
	"sync"
	"time"
)

// Lockout blocks login attempts per key (username+IP) after repeated failures, with an
// exponential cooldown. Keyed by IP as well so an attacker cannot lock a user out from
// elsewhere, and never surfaced to the client (the login just fails as usual).
type Lockout struct {
	mu        sync.Mutex
	entries   map[string]*lockEntry
	Threshold int           // failures before the first lock
	Base      time.Duration // first cooldown; doubles per lock, capped at Max
	Max       time.Duration
	now       func() time.Time
}

type lockEntry struct {
	failures int
	locks    int
	until    time.Time
	seen     time.Time
}

func NewLockout(threshold int, base time.Duration) *Lockout {
	return &Lockout{entries: map[string]*lockEntry{}, Threshold: threshold, Base: base, Max: 24 * time.Hour, now: time.Now}
}

// Locked reports whether key is currently blocked.
func (l *Lockout) Locked(key string) (bool, time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	if e == nil || !l.now().Before(e.until) {
		return false, time.Time{}
	}
	return true, e.until
}

// Fail records a failed attempt; returns the lock end if this failure triggered a lock.
func (l *Lockout) Fail(key string) (locked bool, until time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.pruneLocked(now)
	e := l.entries[key]
	if e == nil {
		e = &lockEntry{}
		l.entries[key] = e
	}
	e.seen = now
	e.failures++
	if e.failures < l.Threshold {
		return false, time.Time{}
	}
	e.failures = 0
	e.locks++
	d := l.Base
	for i := 1; i < e.locks && d < l.Max; i++ {
		d *= 2
	}
	if d > l.Max {
		d = l.Max
	}
	e.until = now.Add(d)
	return true, e.until
}

// Reset forgets the key (successful login).
func (l *Lockout) Reset(key string) {
	l.mu.Lock()
	delete(l.entries, key)
	l.mu.Unlock()
}

// pruneLocked drops entries idle for longer than Max (caller holds mu).
func (l *Lockout) pruneLocked(now time.Time) {
	if len(l.entries) < 1000 {
		return
	}
	for k, e := range l.entries {
		if now.Sub(e.seen) > l.Max && !now.Before(e.until) {
			delete(l.entries, k)
		}
	}
}
