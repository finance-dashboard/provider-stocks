package internal

import (
	"sync"
)

type Cache struct {
	data map[string]Update
	m    sync.Mutex
}

func NewCache() *Cache {
	return &Cache{
		data: make(map[string]Update),
		m:    sync.Mutex{},
	}
}

func (c *Cache) Set(u Update) {
	c.m.Lock()
	defer c.m.Unlock()
	c.data[u.Ticker] = u
}

func (c *Cache) Snapshot() []Update {
	c.m.Lock()
	defer c.m.Unlock()

	snapshot := make([]Update, 0, len(c.data))
	for _, v := range c.data {
		snapshot = append(snapshot, v)
	}

	return snapshot
}
