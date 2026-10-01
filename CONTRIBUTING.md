# Contributing to tickcache

## Setup

```shell
git clone https://github.com/Wsh7Ash/tickcache.git
cd tickcache
go test -race ./...
go vet ./...
```

## Ground rules

- **Zero dependencies** — stdlib only. New deps will not be merged.
- Deterministic tests: use `FakeClock` instead of sleeping; any test that
  sleeps to await expiry will be rejected.
- Public API changes require an example (`Example*` function) and doc updates.
- Run tests with `-race` locally; CI does.

## Pull requests

1. Branch from `main`.
2. Add/adjust tests; keep coverage of concurrency paths.
3. `go vet ./... && go test -race ./...` green.
4. Update `docs/` and `CHANGELOG.md` under "Unreleased".

## Reporting issues

Include a minimal reproduction, Go version (`go version`), and whether the
issue reproduces with `FakeClock` (deterministic) or only the real clock
(timing-sensitive).
