# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-10-01

### Added
- Generic `Cache[K, V]`: LRU eviction, TTL expiry, stats, purge, delete.
- `Clock` interface with `NewRealClock` and deterministic `FakeClock`/`Advance`.
- `GetOrCompute` with per-key call collapsing (singleflight-style).
- Optional background sweeper with `Close()` lifecycle.
- `OnEvict` hook with capacity/ttl/manual reasons.
- Test suite: 16 tests incl. `-race` concurrent stress, deterministic TTL.
