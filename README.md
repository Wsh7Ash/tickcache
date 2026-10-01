# tickcache

**tickcache** is a dependency-free Go library providing a thread-safe generic
LRU + TTL cache with an injectable clock — so expiry logic is testable without
sleeping, and time behaves deterministically in CI.

## The problem

Most Go caching libraries wire `time.Now()` deep inside, which means expiry
behavior can only be tested by actually waiting, tests get flaky, and you
cannot simulate hours of TTL drift in microseconds. tickcache treats time as a
dependency: bring your own `Clock`, or use the `FakeClock` shipped in the box
to advance time instantly in tests.

## Key features

- **Generics** — `Cache[K comparable, V any]`, no `interface{}` casting.
- **Thread-safe** — single mutex over a `map` + `container/list` LRU; every
  public method is safe for concurrent use.
- **TTL with lazy + periodic eviction** — expired entries are dropped on read
  and by a background sweeper (configurable interval).
- **Injectable clock** — `NewClock()` (real) or `FakeClock` with
  `Advance(d)` for deterministic tests.
- **LRU eviction with capacity** — insertion-order eviction with O(1)
  `list.MoveToFront`.
- **Rich stats** — hits, misses, evictions (capacity + TTL), plus `HitRate()`.
- **Load-or-compute** — `GetOrCompute` with singleflight-style per-key mutexes
  to collapse concurrent misses for the same key.
- **Metrics snapshot + live hooks** — `Stats()`, `OnEvict` callback.
- **Zero dependencies** — stdlib only; Go 1.21+.

## Architecture summary

```text
tickcache/
├─ cache.go        # Cache[K,V]: Get/Set/Delete/GetOrCompute/Len/Stats
├─ clock.go        # Clock interface, realClock, FakeClock (Advance/Since)
├─ singleflight.go # keyGroup: per-key call collapsing
├─ example_test.go # godoc examples
└─ cache_test.go   # race-tested unit tests incl. deterministic TTL via FakeClock
```

Core structure: `map[K]*list.Element` where each element holds
`entry{key, value, expiresAt}`. LRU order lives in `container/list`; expiry
checks compare `clock.Now()` against `entry.expiresAt`.

## Requirements

- Go 1.21+

## Installation

```shell
go get github.com/Wsh7Ash/tickcache
```

## Quick start

```go
package main

import (
	"fmt"
	"time"

	"github.com/Wsh7Ash/tickcache"
)

func main() {
	c := tickcache.New[string, int](tickcache.Options[string, int]{
		Capacity:     100,
		TTL:          5 * time.Minute,
		SweepInterval: time.Minute,
	})

	c.Set("a", 1)
	v, ok := c.Get("a")     // 1, true
	fmt.Println(v, ok)
}
```

Deterministic TTL test (no sleeping!):

```go
func TestExpiry(t *testing.T) {
	clk := tickcache.NewFakeClock(time.Unix(0, 0))
	c := tickcache.New[int, string](tickcache.Options[int, string]{
		Capacity: 10, TTL: time.Minute, Clock: clk,
	})
	c.Set(1, "x")
	clk.Advance(2 * time.Minute)      // instant time travel
	if _, ok := c.Get(1); ok {
		t.Fatal("entry should have expired")
	}
}
```

## Configuration

`Options[K, V]`:

| Field | Default | Meaning |
|---|---|---|
| `Capacity` | 128 | max entries; oldest is evicted beyond this |
| `TTL` | 0 (no expiry) | entry lifetime |
| `Clock` | real clock | any `func() time.Time`-compatible implementation |
| `SweepInterval` | 0 (off) | background cleanup cadence; `Close()` stops it |

## Testing

```shell
go test -race ./...
go vet ./...
```

## Project structure

See "Architecture summary"; deeper detail in `docs/ARCHITECTURE.md` and
per-function docs in the source.

## Limitations

- Values are not copied on read; store immutable values or clone on Set.
- Single-node only; no persistence, no cross-process coherence.
- Sweeper granularity means TTL expiry can lag up to one sweep interval for
  unread keys.

## Roadmap

- Optional jittered TTL
- Weighted capacity (size function instead of entry count)
- `Peek` variants that don't touch LRU order

## Security notes

The library performs no I/O, spawns no goroutines unless `SweepInterval > 0`,
and never retains values beyond eviction (the `OnEvict` hook receives them;
set it to nil if that matters for your key material).

## License

MIT
