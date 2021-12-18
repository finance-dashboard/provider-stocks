package internal

import (
	"sync"

	uuid "github.com/satori/go.uuid"
)

type Subscriber struct {
	ID      uuid.UUID
	Updates chan Update
}

type SubscriberList struct {
	data map[Subscriber]struct{}
	m    sync.RWMutex
}

func NewSubscriberList() *SubscriberList {
	return &SubscriberList{
		data: make(map[Subscriber]struct{}),
		m:    sync.RWMutex{},
	}
}

func (l *SubscriberList) Subscribe() Subscriber {
	subscriber := Subscriber{
		ID:      uuid.NewV4(),
		Updates: make(chan Update, 10),
	}

	l.m.Lock()
	defer l.m.Unlock()
	l.data[subscriber] = struct{}{}

	return subscriber
}

func (l *SubscriberList) Unsubscribe(s Subscriber) {
	l.m.Lock()
	defer l.m.Unlock()
	delete(l.data, s)
}

func (l *SubscriberList) Snapshot() []Subscriber {
	l.m.RLock()
	defer l.m.RUnlock()

	snapshot := make([]Subscriber, 0, len(l.data))
	for v := range l.data {
		snapshot = append(snapshot, v)
	}

	return snapshot
}

func (l *SubscriberList) Len() int {
	l.m.RLock()
	defer l.m.RUnlock()
	return len(l.data)
}
