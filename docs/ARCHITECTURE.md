# tickcache architecture

## Structure

A `Cache[K,V]` is:

```text
map[K]*list.Element   ← O(1) lookup by key
container/list        ← recency order; front = MRU, back = LRU
Element.Value         ← *entry{ key K; value V; expiresAt time.Time }
```

One `sync.Mutex` guards both structures. Operations are short (no I/O, no
callbacks under lock — eviction hooks are dispatched via goroutine), so lock
contention stays low.

## Time model

`Clock` is an interface with one method, `Now() time.Time`:

- `NewRealClock()` — wraps `time.Now()`, used by default.
- `NewFakeClock(base)` — test clock; `Advance(d)` moves time forward
  instantly (negative values panic: reversed time is always a bug).

Because `Set` stamps `expiresAt = clock.Now() + TTL` and reads compare against
`clock.Now()`, expiry behavior is a pure function of the clock — tests can
cross a TTL boundary in nanoseconds, with zero flake.

## Eviction paths

| Path | Trigger | Reason |
|---|---|---|
| lazy | `Get`/`Peek` finds `now > expiresAt` | `EvictTTL` |
| capacity | `Set` pushes `Len()` past `Capacity` | `EvictCapacity` (oldest first) |
| sweep | background ticker calls `sweepLocked` | `EvictTTL` per expired entry |
| manual | `Delete` / `Purge` | `EvictManual` (not counted in capacity/TTL stats) |

The sweeper walks back-to-front (oldest first) and stops touching entries as
soon as none are expired on the remaining segment — cheap when few entries
expire per interval.

## GetOrCompute / keyGroup

A two-level strategy:

1. Fast path: plain `Get` hit returns immediately.
2. Slow path: `keyGroup.do` keeps `map[K]*callState`; the first caller for a
   key runs `compute`, stores the result, then releases waiters. Followers
   block on a `WaitGroup` and read through the cache afterwards (falling back
   to the shared result if it was evicted in between).

Errors from `compute` are returned to all callers of that generation but never
cached — a transient failure must not poison the key.

## Concurrency contract

- All exported methods are safe for concurrent use.
- `OnEvict` may be invoked concurrently from goroutines; it must not call back
  into the same cache synchronously (it receives values after eviction).
- `Close` is idempotent and waits for the sweeper goroutine to exit; other
  methods become no-ops after close (Get/Set) — deliberate: a cache shutting
  down mid-request must not panic its callers.

## Why not shards?

Sharded caches reduce contention at high core counts but complicate LRU
guarantees and stats exactness. tickcache targets correctness and
deterministic testability first; the global mutex is fine up to six figures of
ops/sec, and short critical sections keep it there.
