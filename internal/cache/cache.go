package cache

import (
	"sync"

	"finance-dashboard-provider-stocks/internal/update"
)

type Cache struct {
	data map[string]update.Update
	m    sync.Mutex
}

func New() *Cache {
	return &Cache{
		data: make(map[string]update.Update),
		m:    sync.Mutex{},
	}
}

func (c *Cache) Set(u update.Update) {
	c.m.Lock()
	defer c.m.Unlock()
	c.data[u.Ticker] = u
}

func (c *Cache) Snapshot() []update.Update {
	c.m.Lock()
	defer c.m.Unlock()

	snapshot := make([]update.Update, 0, len(c.data))
	for _, v := range c.data {
		snapshot = append(snapshot, v)
	}

	return snapshot
}
