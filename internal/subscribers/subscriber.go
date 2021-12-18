package subscribers

import (
	"sync"

	"finance-dashboard-provider-stocks/internal/update"

	uuid "github.com/satori/go.uuid"
)

type Subscriber struct {
	ID      uuid.UUID
	Updates chan update.Update
}

type Subscribers struct {
	data map[Subscriber]struct{}
	m    sync.RWMutex
}

func New() *Subscribers {
	return &Subscribers{
		data: make(map[Subscriber]struct{}),
		m:    sync.RWMutex{},
	}
}

func (l *Subscribers) Subscribe() Subscriber {
	subscriber := Subscriber{
		ID:      uuid.NewV4(),
		Updates: make(chan update.Update, 10),
	}

	l.m.Lock()
	defer l.m.Unlock()
	l.data[subscriber] = struct{}{}

	return subscriber
}

func (l *Subscribers) Unsubscribe(s Subscriber) {
	l.m.Lock()
	defer l.m.Unlock()
	delete(l.data, s)
}

func (l *Subscribers) Snapshot() []Subscriber {
	l.m.RLock()
	defer l.m.RUnlock()

	snapshot := make([]Subscriber, 0, len(l.data))
	for v := range l.data {
		snapshot = append(snapshot, v)
	}

	return snapshot
}

func (l *Subscribers) Len() int {
	l.m.RLock()
	defer l.m.RUnlock()
	return len(l.data)
}
