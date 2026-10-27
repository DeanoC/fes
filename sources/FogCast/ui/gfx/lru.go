package gfx

import "container/list"

// lru holds at most n values and about budget bytes, dropping the least
// recently used.
type lru[K comparable, V any] struct {
	n, budget, bytes int
	order            *list.List
	entries          map[K]*list.Element
}

type lruEntry[K comparable, V any] struct {
	key   K
	value V
	bytes int
}

func newLRU[K comparable, V any](n, budget int) *lru[K, V] {
	return &lru[K, V]{n: n, budget: budget, order: list.New(), entries: map[K]*list.Element{}}
}

func (c *lru[K, V]) get(key K) (V, bool) {
	e := c.entries[key]
	if e == nil {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*lruEntry[K, V]).value, true
}

// put adds value, which holds about bytes.
func (c *lru[K, V]) put(key K, value V, bytes int) {
	c.entries[key] = c.order.PushFront(&lruEntry[K, V]{key, value, bytes})
	c.bytes += bytes
	for c.order.Len() > c.n || c.bytes > c.budget && c.order.Len() > 1 {
		last := c.order.Back()
		entry := last.Value.(*lruEntry[K, V])
		c.order.Remove(last)
		delete(c.entries, entry.key)
		c.bytes -= entry.bytes
	}
}
