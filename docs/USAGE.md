# tickcache usage guide

## Choosing options

| Need | Configuration |
|---|---|
| Plain bounded memo | `Capacity: N` |
| Session-like expiry | `Capacity: N, TTL: 30*time.Minute` |
| Deterministic tests | `Clock: NewFakeClock(base)` |
| Bound memory of long-lived cold entries | `SweepInterval: TTL/4` |
| Counters or cleanup on eviction | `OnEvict: func(k, v, reason)` |

## Patterns

### HTTP handler memo

```go
var userCache = tickcache.New[int64, *User](tickcache.Options[int64, *User]{
	Capacity:      10_000,
	TTL:           time.Minute,
	SweepInterval: 15 * time.Second,
})

func getUser(ctx context.Context, id int64) (*User, error) {
	return userCache.GetOrCompute(id, func(id int64) (*User, error) {
		return db.LoadUser(ctx, id)
	})
}
```

`GetOrCompute` collapses concurrent misses for the same id — a thundering herd
on one cold key triggers one DB query.

### Deterministic TTL test

```go
func TestSessionExpires(t *testing.T) {
	clk := tickcache.NewFakeClock(time.Unix(0, 0))
	c := tickcache.New[string, string](tickcache.Options[string, string]{
		TTL: time.Hour, Clock: clk,
	})
	defer c.Close()
	c.Set("sid", "user7")
	clk.Advance(90 * time.Minute)
	if _, ok := c.Get("sid"); ok {
		t.Fatal("session must expire")
	}
}
```

### Interpreting stats

```go
s := c.Stats()
fmt.Printf("hit rate %.1f%%, %d capacity evictions, %d ttl evictions\n",
	s.HitRate()*100, s.EvictedCapacity, s.EvictedTTL)
```

- High `EvictedCapacity` → raise `Capacity` or accept churn.
- High `EvictedTTL` with low hit rate → TTL likely too short for the workload.

## Pitfalls

- `Peek` deliberately does not refresh recency; use `Get` unless you want
  scan operations to leave LRU order untouched.
- Values are shared, not copied: don't mutate a value after `Set`.
- `SweepInterval` only bounds how long *unread* expired entries occupy memory;
  correctness of reads is unaffected (lazy check always runs).
- Call `Close()` for caches with a sweeper (e.g. in test cleanup) or the
  goroutine leaks for the process lifetime.
