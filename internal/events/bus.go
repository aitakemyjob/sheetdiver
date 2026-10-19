// Package events is a tiny fan-out bus that carries backend notifications
// (agent activity, new proposals, "show this" requests) to every UI client.
package events

import (
	"sync"
	"time"
)

type Event struct {
	Type string    `json:"type"`
	Time time.Time `json:"time"`
	Data any       `json:"data,omitempty"`
}

type Bus struct {
	mu   sync.Mutex
	subs map[int]chan Event
	next int
}

// NewBus creates an empty event bus.
func NewBus() *Bus { return &Bus{subs: map[int]chan Event{}} }

// Subscribe returns a channel of events and a cancel func. Slow subscribers
// drop events rather than block publishers.
func (b *Bus) Subscribe() (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.next
	b.next++
	ch := make(chan Event, 256)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
	}
}

// Publish sends an event to every subscriber without blocking; events for
// subscribers whose buffers are full are dropped.
func (b *Bus) Publish(typ string, data any) {
	ev := Event{Type: typ, Time: time.Now(), Data: data}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}
