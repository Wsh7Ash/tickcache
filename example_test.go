package tickcache_test

import (
	"fmt"
	"time"

	"github.com/Wsh7Ash/tickcache"
)

func Example_basic() {
	c := tickcache.New[string, int](tickcache.Options[string, int]{
		Capacity: 16,
		TTL:      5 * time.Minute,
	})
	c.Set("hits", 42)
	v, ok := c.Get("hits")
	fmt.Println(v, ok)
	// Output: 42 true
}

func ExampleFakeClock() {
	base := time.Unix(1_700_000_000, 0)
	clk := tickcache.NewFakeClock(base)
	c := tickcache.New[string, string](tickcache.Options[string, string]{
		Capacity: 4,
		TTL:      time.Minute,
		Clock:    clk,
	})
	c.Set("k", "v")
	clk.Advance(2 * time.Minute)
	_, ok := c.Get("k")
	fmt.Println("after advance:", ok)
	// Output: after advance: false
}

func ExampleCache_GetOrCompute() {
	c := tickcache.New[int, string](tickcache.Options[int, string]{Capacity: 8})
	v, _ := c.GetOrCompute(1, func(k int) (string, error) {
		return fmt.Sprintf("computed for %d", k), nil
	})
	fmt.Println(v)
	// Output: computed for 1
}
