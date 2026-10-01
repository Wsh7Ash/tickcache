# Results

## Environment
- OS: Windows 11 (bash / msys2), x86-64
- Runtime/compiler: Go 1.27.0 windows/amd64
- Dependency manager: go modules (zero dependencies)
- Test framework: go test (stdlib), with `-race`

## Validation performed
- install: PASS (`go mod download` — nothing to fetch)
- build: PASS (`go vet ./...` clean, `go build` implicit via test)
- unit tests: PASS (16 tests + 3 examples)
- integration tests: N/A (library)
- lint: PASS (`go vet`)
- type check: PASS (compiler)
- concurrency: PASS (`go test -race -count=1 ./...`)
- demo run: PASS (example tests executed as part of `go test`)

## Commands executed
```shell
go vet ./...
# (no output, exit 0)

go test -race -count=1 ./...
# ok  github.com/Wsh7Ash/tickcache  1.301s
```

## Observed result
All 16 tests pass under the race detector, including:

- deterministic TTL expiry via `FakeClock.Advance` (no sleeps)
- TTL refresh-on-Set semantics
- LRU eviction order, and `Peek` provably not refreshing recency
- `GetOrCompute` collapsing 50 concurrent misses for one key into a single
  compute (verified with an atomic counter)
- failed computes not cached
- background sweeper removing expired entries and `Close()` being idempotent
- 32-goroutine × 200-op mixed access stress with capacity never exceeded

## Generated artifacts
None beyond the test run itself (library project; the examples in
`example_test.go` double as the demo and are executed by `go test`).

## Known limitations
- Values are shared, not copied (documented).
- Sweeper cadence means unread expired entries can linger up to one interval.
- Single global mutex: correctness-first design; see docs/ARCHITECTURE.md.
