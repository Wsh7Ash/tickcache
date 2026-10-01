package tickcache

import "sync"

// keyGroup collapses concurrent GetOrCompute calls for the same key so the
// compute function runs once; the winner stores, followers read.
type keyGroup[K comparable, V any] struct {
	mu    sync.Mutex
	owner *Cache[K, V]
	calls map[K]*callState[K, V]
}

type callState[K comparable, V any] struct {
	wg    sync.WaitGroup
	value V
	err   error
}

func newKeyGroup[K comparable, V any](owner *Cache[K, V]) *keyGroup[K, V] {
	return &keyGroup[K, V]{owner: owner, calls: make(map[K]*callState[K, V])}
}

func (g *keyGroup[K, V]) do(key K, compute func(K) (V, error)) (V, error) {
	g.mu.Lock()
	if st, ok := g.calls[key]; ok {
		g.mu.Unlock()
		st.wg.Wait()
		// The winner stored the value; read through the cache (or fall back
		// to the shared result if it was evicted between store and read).
		if v, ok := g.owner.Get(key); ok {
			return v, nil
		}
		return st.value, st.err
	}
	st := &callState[K, V]{}
	st.wg.Add(1)
	g.calls[key] = st
	g.mu.Unlock()

	v, err := compute(key)
	if err == nil {
		g.owner.Set(key, v)
	}

	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()

	st.value = v
	st.err = err
	st.wg.Done()
	return v, err
}
