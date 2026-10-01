package tickcache

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSetGet(t *testing.T) {
	c := New[string, int](Options[string, int]{})
	c.Set("a", 1)
	v, ok := c.Get("a")
	if !ok || v != 1 {
		t.Fatalf("got %d,%v want 1,true", v, ok)
	}
}

func TestMiss(t *testing.T) {
	c := New[string, int](Options[string, int]{})
	if _, ok := c.Get("nope"); ok {
		t.Fatal("expected miss")
	}
	if c.Stats().Misses != 1 {
		t.Fatalf("misses = %d, want 1", c.Stats().Misses)
	}
}

func TestTTLExpiryDeterministic(t *testing.T) {
	clk := NewFakeClock(time.Unix(0, 0))
	c := New[int, string](Options[int, string]{Capacity: 10, TTL: time.Minute, Clock: clk})
	c.Set(1, "x")
	if _, ok := c.Get(1); !ok {
		t.Fatal("expected hit before TTL")
	}
	clk.Advance(61 * time.Second)
	if _, ok := c.Get(1); ok {
		t.Fatal("expected miss after TTL")
	}
	if c.Stats().EvictedTTL != 1 {
		t.Fatalf("EvictedTTL = %d, want 1", c.Stats().EvictedTTL)
	}
}

func TestTTLRefreshedOnSet(t *testing.T) {
	clk := NewFakeClock(time.Unix(0, 0))
	c := New[int, string](Options[int, string]{TTL: time.Minute, Clock: clk})
	c.Set(1, "a")
	clk.Advance(50 * time.Second)
	c.Set(1, "b") // refresh
	clk.Advance(50 * time.Second)
	if v, ok := c.Get(1); !ok || v != "b" {
		t.Fatalf("refresh failed: %q %v", v, ok)
	}
}

func TestNoTTLWhenZero(t *testing.T) {
	clk := NewFakeClock(time.Unix(0, 0))
	c := New[int, string](Options[int, string]{Clock: clk})
	c.Set(1, "x")
	clk.Advance(100 * time.Hour)
	if _, ok := c.Get(1); !ok {
		t.Fatal("entry with TTL=0 must never expire")
	}
}

func TestLRUEviction(t *testing.T) {
	c := New[int, int](Options[int, int]{Capacity: 2})
	c.Set(1, 1)
	c.Set(2, 2)
	c.Get(1)      // 1 now most recent; 2 is LRU
	c.Set(3, 3)   // evicts 2
	if _, ok := c.Get(2); ok {
		t.Fatal("2 should have been evicted")
	}
	if _, ok := c.Get(1); !ok {
		t.Fatal("1 should have survived")
	}
	if c.Stats().EvictedCapacity != 1 {
		t.Fatalf("EvictedCapacity = %d, want 1", c.Stats().EvictedCapacity)
	}
}

func TestSetUpdateDoesNotEvict(t *testing.T) {
	c := New[int, int](Options[int, int]{Capacity: 2})
	c.Set(1, 1)
	c.Set(2, 2)
	c.Set(1, 10) // update, not insert
	if c.Len() != 2 {
		t.Fatalf("len = %d, want 2", c.Len())
	}
	if v, _ := c.Get(1); v != 10 {
		t.Fatalf("updated value = %d, want 10", v)
	}
}

func TestPeekDoesNotRefreshRecency(t *testing.T) {
	c := New[int, int](Options[int, int]{Capacity: 2})
	c.Set(1, 1)
	c.Set(2, 2)
	c.Peek(1)   // must NOT make 1 most-recent
	c.Set(3, 3) // evicts LRU = 1
	if _, ok := c.Peek(1); ok {
		t.Fatal("1 should have been evicted (peek must not refresh)")
	}
	if _, ok := c.Peek(2); !ok {
		t.Fatal("2 should have survived")
	}
}

func TestDeleteAndPurge(t *testing.T) {
	c := New[int, int](Options[int, int]{})
	c.Set(1, 1)
	c.Delete(1)
	if _, ok := c.Get(1); ok {
		t.Fatal("deleted key must miss")
	}
	c.Set(2, 2)
	c.Set(3, 3)
	c.Purge()
	if c.Len() != 0 {
		t.Fatalf("len after purge = %d", c.Len())
	}
	if c.Stats().EvictedTTL+c.Stats().EvictedCapacity != 0 {
		t.Fatal("manual evictions must not count as capacity/ttl")
	}
}

func TestOnEvictHook(t *testing.T) {
	var evicted atomic.Int32
	c := New[int, int](Options[int, int]{
		Capacity: 1,
		OnEvict:  func(k, v int, r EvictReason) { evicted.Add(1) },
	})
	c.Set(1, 1)
	c.Set(2, 2)
	// hook runs in a goroutine; wait briefly
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && evicted.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	if evicted.Load() != 1 {
		t.Fatalf("evictions seen = %d, want 1", evicted.Load())
	}
}

func TestGetOrCompute(t *testing.T) {
	c := New[string, string](Options[string, string]{})
	calls := 0
	v, err := c.GetOrCompute("k", func(k string) (string, error) {
		calls++
		return "computed-" + k, nil
	})
	if err != nil || v != "computed-k" {
		t.Fatalf("got %q %v", v, err)
	}
	_, _ = c.GetOrCompute("k", func(k string) (string, error) {
		calls++
		return "should-not-run", nil
	})
	if calls != 1 {
		t.Fatalf("compute called %d times, want 1", calls)
	}
}

func TestGetOrComputeCollapsesConcurrency(t *testing.T) {
	c := New[int, int](Options[int, int]{})
	var calls atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = c.GetOrCompute(7, func(k int) (int, error) {
				calls.Add(1)
				time.Sleep(20 * time.Millisecond) // widen the race window
				return k * 2, nil
			})
		}()
	}
	close(start)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("compute ran %d times, want 1", calls.Load())
	}
}

func TestGetOrComputeErrorNotCached(t *testing.T) {
	c := New[int, int](Options[int, int]{})
	sentinel := errors.New("boom")
	if _, err := c.GetOrCompute(1, func(int) (int, error) { return 0, sentinel }); !errors.Is(err, sentinel) {
		t.Fatalf("err = %v", err)
	}
	if _, ok := c.Get(1); ok {
		t.Fatal("failed compute must not be cached")
	}
}

func TestSweeperRemovesExpired(t *testing.T) {
	clk := NewFakeClock(time.Unix(0, 0))
	c := New[int, int](Options[int, int]{TTL: time.Second, Clock: clk, SweepInterval: 10 * time.Millisecond})
	defer c.Close()
	c.Set(1, 1)
	clk.Advance(2 * time.Second)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && c.Len() > 0 {
		time.Sleep(time.Millisecond)
	}
	if c.Len() != 0 {
		t.Fatal("sweeper did not remove expired entry")
	}
	if c.Stats().EvictedTTL == 0 {
		t.Fatal("sweeper eviction not counted")
	}
}

func TestCloseStopsSweeper(t *testing.T) {
	c := New[int, int](Options[int, int]{SweepInterval: 5 * time.Millisecond})
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal("Close must be idempotent")
	}
}

func TestHitRate(t *testing.T) {
	c := New[int, int](Options[int, int]{})
	c.Set(1, 1)
	c.Get(1)
	c.Get(1)
	c.Get(99)
	if got := c.Stats().HitRate(); got != 2.0/3.0 {
		t.Fatalf("hit rate = %v, want %v", got, 2.0/3.0)
	}
}

func TestFakeClockAdvanceNegativePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("negative Advance must panic")
		}
	}()
	NewFakeClock(time.Unix(0, 0)).Advance(-time.Second)
}

func TestConcurrentMixedAccess(t *testing.T) {
	c := New[int, int](Options[int, int]{Capacity: 64, TTL: time.Minute})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				k := (base*200 + j) % 100
				c.Set(k, j)
				c.Get(k)
				c.Peek(k)
				if j%50 == 0 {
					c.Delete(k)
				}
			}
		}(i)
	}
	wg.Wait()
	if c.Len() > 64 {
		t.Fatalf("len %d exceeds capacity", c.Len())
	}
}

func ExampleCache() {
	c := New[string, int](Options[string, int]{Capacity: 16})
	c.Set("answer", 42)
	v, _ := c.Get("answer")
	fmt.Println(v)
	// Output: 42
}
