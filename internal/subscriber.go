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
	data []Subscriber
	m    sync.RWMutex
}

func (l *SubscriberList) Subscribe() Subscriber {
	subscriber := Subscriber{
		ID:      uuid.NewV4(),
		Updates: make(chan Update, 10),
	}

	l.m.Lock()
	defer l.m.Unlock()
	l.data = append(l.data, subscriber)

	return subscriber
}

func (l *SubscriberList) Unsubscribe(s Subscriber) {
	l.m.Lock()
	defer l.m.Unlock()

	for i := 0; i < len(l.data); i++ {
		if l.data[i].ID == s.ID {
			l.data = append(l.data[:i], l.data[i+1:]...)
			return
		}
	}
}

func (l *SubscriberList) Snapshot() []Subscriber {
	l.m.RLock()
	defer l.m.RUnlock()

	data := make([]Subscriber, len(l.data))
	copy(data, l.data)
	return data
}
