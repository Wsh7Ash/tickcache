package tickcache

import (
	"container/list"
	"errors"
	"sync"
	"time"
)

// EvictReason explains why an entry left the cache.
type EvictReason string

const (
	EvictCapacity EvictReason = "capacity"
	EvictTTL      EvictReason = "ttl"
	EvictManual   EvictReason = "manual"
)

// Options configures a Cache. Zero-value fields select defaults.
type Options[K comparable, V any] struct {
	// Capacity is the maximum number of entries. Default 128.
	Capacity int
	// TTL is the entry lifetime; 0 disables time-based expiry.
	TTL time.Duration
	// Clock supplies time; default real clock.
	Clock Clock
	// SweepInterval > 0 starts a background goroutine that removes expired
	// entries. Stop it with Close. Default 0 (no sweeper).
	SweepInterval time.Duration
	// OnEvict, when non-nil, is invoked for every eviction with the key,
	// value and reason. Invoked while the cache lock is NOT held.
	OnEvict func(key K, value V, reason EvictReason)
}

type entry[K comparable, V any] struct {
	key       K
	value     V
	expiresAt time.Time // zero when TTL disabled
}

// Stats is a point-in-time snapshot of cache counters.
type Stats struct {
	Hits             uint64
	Misses           uint64
	EvictedCapacity  uint64
	EvictedTTL       uint64
	CurrentEntries   int
}

// HitRate returns hits / (hits+misses); 0 when no lookups yet.
func (s Stats) HitRate() float64 {
	total := s.Hits + s.Misses
	if total == 0 {
		return 0
	}
	return float64(s.Hits) / float64(total)
}

// ErrClosed is returned by operations after Close.
var ErrClosed = errors.New("tickcache: cache is closed")

// Cache is a thread-safe generic LRU cache with optional TTL. All public
// methods are safe for concurrent use. Create with New.
type Cache[K comparable, V any] struct {
	mu       sync.Mutex
	items    map[K]*list.Element
	order    *list.List // front = most recently used
	capacity int
	ttl      time.Duration
	clock    Clock
	onEvict  func(K, V, EvictReason)
	groups   *keyGroup[K, V]

	sweepStop chan struct{}
	sweepDone chan struct{}
	closed    bool

	hits, misses, evCap, evTTL uint64
}

// New builds a Cache from opts.
func New[K comparable, V any](opts Options[K, V]) *Cache[K, V] {
	if opts.Capacity <= 0 {
		opts.Capacity = 128
	}
	clk := opts.Clock
	if clk == nil {
		clk = NewRealClock()
	}
	c := &Cache[K, V]{
		items:    make(map[K]*list.Element, opts.Capacity),
		order:    list.New(),
		capacity: opts.Capacity,
		ttl:      opts.TTL,
		clock:    clk,
		onEvict:  opts.OnEvict,
	}
	c.groups = newKeyGroup(c)
	if opts.SweepInterval > 0 {
		c.sweepStop = make(chan struct{})
		c.sweepDone = make(chan struct{})
		go c.sweepLoop(opts.SweepInterval)
	}
	return c
}

// touchLRGBehindLock moves an element to the front. Caller holds mu.
func (c *Cache[K, V]) touch(el *list.Element) {
	c.order.MoveToFront(el)
}

// evictLocked removes el and records the reason. Caller holds mu.
func (c *Cache[K, V]) evictLocked(el *list.Element, reason EvictReason) {
	c.order.Remove(el)
	ent := el.Value.(*entry[K, V])
	delete(c.items, ent.key)
	switch reason {
	case EvictCapacity:
		c.evCap++
	case EvictTTL:
		c.evTTL++
	}
	if c.onEvict != nil {
		hook := c.onEvict
		go hook(ent.key, ent.value, reason)
	}
}

func (c *Cache[K, V]) expiredLocked(ent *entry[K, V], now time.Time) bool {
	return !ent.expiresAt.IsZero() && now.After(ent.expiresAt)
}

// Get returns the value for key and whether it was present and unexpired.
// A hit refreshes LRU recency.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		var zero V
		return zero, false
	}
	el, ok := c.items[key]
	if !ok {
		c.misses++
		var zero V
		return zero, false
	}
	ent := el.Value.(*entry[K, V])
	if c.expiredLocked(ent, c.clock.Now()) {
		c.evictLocked(el, EvictTTL)
		c.misses++
		var zero V
		return zero, false
	}
	c.hits++
	c.touch(el)
	return ent.value, true
}

// Peek returns the value without refreshing LRU recency.
func (c *Cache[K, V]) Peek(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		var zero V
		return zero, false
	}
	el, ok := c.items[key]
	if !ok {
		return c.missPeek()
	}
	ent := el.Value.(*entry[K, V])
	if c.expiredLocked(ent, c.clock.Now()) {
		c.evictLocked(el, EvictTTL)
		return c.missPeek()
	}
	return ent.value, true
}

func (c *Cache[K, V]) missPeek() (zero V, _ bool) {
	return zero, false
}

// Set inserts or updates key, evicting the LRU tail beyond capacity.
func (c *Cache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	now := c.clock.Now()
	var expiresAt time.Time
	if c.ttl > 0 {
		expiresAt = now.Add(c.ttl)
	}
	if el, ok := c.items[key]; ok {
		ent := el.Value.(*entry[K, V])
		ent.value = value
		ent.expiresAt = expiresAt
		c.touch(el)
		return
	}
	el := c.order.PushFront(&entry[K, V]{key: key, value: value, expiresAt: expiresAt})
	c.items[key] = el
	for c.order.Len() > c.capacity {
		tail := c.order.Back()
		c.evictLocked(tail, EvictCapacity)
	}
}

// Delete removes key if present.
func (c *Cache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if el, ok := c.items[key]; ok {
		c.evictLocked(el, EvictManual)
	}
}

// Len returns the number of entries currently stored (including possibly
// expired-but-not-yet-evicted ones).
func (c *Cache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// Stats snapshots the counters.
func (c *Cache[K, V]) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{
		Hits:            c.hits,
		Misses:          c.misses,
		EvictedCapacity: c.evCap,
		EvictedTTL:      c.evTTL,
		CurrentEntries:  c.order.Len(),
	}
}

// GetOrCompute returns the cached value, or computes it with compute and
// stores it. Concurrent calls for the same key collapse into one compute.
func (c *Cache[K, V]) GetOrCompute(key K, compute func(K) (V, error)) (V, error) {
	if v, ok := c.Get(key); ok {
		return v, nil
	}
	return c.groups.do(key, compute)
}

// Purge removes all entries.
func (c *Cache[K, V]) Purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	for c.order.Len() > 0 {
		c.evictLocked(c.order.Back(), EvictManual)
	}
}

// Close stops the sweeper (if any) and marks the cache unusable. Idempotent.
func (c *Cache[K, V]) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	stop := c.sweepStop
	done := c.sweepDone
	c.mu.Unlock()

	if stop != nil {
		close(stop)
		<-done
	}
	return nil
}

// sweepLocked removes expired entries; returns how many. Caller holds mu.
func (c *Cache[K, V]) sweepLocked(now time.Time) int {
	removed := 0
	for el := c.order.Back(); el != nil; {
		prev := el.Prev()
		ent := el.Value.(*entry[K, V])
		if c.expiredLocked(ent, now) {
			c.evictLocked(el, EvictTTL)
			removed++
		}
		el = prev
	}
	return removed
}

func (c *Cache[K, V]) sweepLoop(interval time.Duration) {
	defer close(c.sweepDone)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.sweepStop:
			return
		case <-ticker.C:
			c.mu.Lock()
			if !c.closed {
				c.sweepLocked(c.clock.Now())
			}
			c.mu.Unlock()
		}
	}
}
