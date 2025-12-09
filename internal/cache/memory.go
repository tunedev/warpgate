package cache

import (
	"context"
	"sync"
	"time"
)

type entry struct {
	key  string
	resp *CachedResponse
	prev *entry
	next *entry
}

type InMemoryCache struct {
	mu         sync.RWMutex
	items      map[string]*entry
	head       *entry
	tail       *entry
	maxEntries int
}

func NewInMemoryCache(maxEntries int) *InMemoryCache {
	if maxEntries <= 0 {
		maxEntries = 1024
	}
	return &InMemoryCache{
		items:      make(map[string]*entry, maxEntries),
		maxEntries: maxEntries,
	}
}

func (c *InMemoryCache) Get(ctx context.Context, key string) (*CachedResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	resp := e.resp

	if !resp.ExpiresAt.IsZero() && time.Now().After(resp.ExpiresAt) {
		c.remove(e)
		delete(c.items, key)
		return nil, false
	}

	c.moveToFront(e)

	return resp, true
}

func (c *InMemoryCache) Set(ctx context.Context, key string, resp *CachedResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.items[key]; ok {
		e.resp = resp
		c.moveToFront(e)
		return
	}

	e := &entry{
		key:  key,
		resp: resp,
	}
	c.items[key] = e
	c.addToFront(e)

	if len(c.items) > c.maxEntries {
		c.evictOldest()
	}
}

func (c *InMemoryCache) Delete(ctx context.Context, key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[key]
	if !ok {
		return
	}
	c.remove(e)
	delete(c.items, key)
}

func (c *InMemoryCache) addToFront(e *entry) {
	e.prev = nil
	e.next = c.head
	if c.head != nil {
		c.head.prev = e
	}
	c.head = e
	if c.tail == nil {
		c.tail = e
	}
}

func (c *InMemoryCache) moveToFront(e *entry) {
	if c.head == e {
		return
	}
	c.remove(e)
	c.addToFront(e)
}

func (c *InMemoryCache) remove(e *entry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
	e.prev = nil
	e.next = nil
}

func (c *InMemoryCache) evictOldest() {
	if c.tail == nil {
		return
	}
	oldest := c.tail
	c.remove(oldest)
	delete(c.items, oldest.key)
}
