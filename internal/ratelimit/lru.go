package ratelimit

import (
	"container/list"

	"golang.org/x/time/rate"
)

type lru struct {
	cap   int
	ll    *list.List
	items map[string]*list.Element
}

type entry struct {
	key string
	lim *rate.Limiter
}

func newLRU(capacity int) *lru {
	return &lru{
		cap:   capacity,
		ll:    list.New(),
		items: make(map[string]*list.Element, capacity),
	}
}

// Returns the limiter for key
func (c *lru) get(key string, make func() *rate.Limiter) *rate.Limiter {
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*entry).lim
	}
	lim := make()
	el := c.ll.PushFront(&entry{key: key, lim: lim})
	c.items[key] = el

	if c.ll.Len() > c.cap {
		oldest := c.ll.Back()
		if oldest != nil {
			c.ll.Remove(oldest)
			delete(c.items, oldest.Value.(*entry).key)
		}
	}
	return lim
}
