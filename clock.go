package tickcache

import (
	"sync"
	"time"
)

// Clock supplies the notion of "now" to a Cache. Injectable so tests can
// advance time deterministically instead of sleeping.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// NewRealClock returns a Clock backed by time.Now.
func NewRealClock() Clock { return realClock{} }

// FakeClock is a controllable Clock for tests. Start it at any base time and
// move it forward with Advance; nothing sleeps, nothing is flaky.
type FakeClock struct {
	mu   sync.Mutex
	now  time.Time
	wait []chan time.Time
}

// NewFakeClock returns a FakeClock pinned at base.
func NewFakeClock(base time.Time) *FakeClock {
	return &FakeClock{now: base}
}

// Now implements Clock.
func (f *FakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the clock forward by d. Panics on negative durations to catch
// reversed-time bugs early.
func (f *FakeClock) Advance(d time.Duration) {
	if d < 0 {
		panic("tickcache: FakeClock.Advance with negative duration")
	}
	f.mu.Lock()
	f.now = f.now.Add(d)
	waiters := f.wait
	f.wait = nil
	f.mu.Unlock()
	for _, ch := range waiters {
		ch <- f.Now()
	}
}

// Since returns the duration from t to the current fake time.
func (f *FakeClock) Since(t time.Time) time.Duration {
	return f.Now().Sub(t)
}
