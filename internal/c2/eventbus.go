package c2

import (
	"sync"
	"sync/atomic"
	"time"
)

// Event is the event unit transported inside the EventBus, a "real-time projection" of database.C2Event.
// The differences are:
//   - the database table keeps all history, for auditing and list pagination;
//   - the EventBus caches only the most recent N, for real-time SSE/WS push to online subscribers.
type Event struct {
	ID        string                 `json:"id"`
	Level     string                 `json:"level"`
	Category  string                 `json:"category"`
	SessionID string                 `json:"sessionId,omitempty"`
	TaskID    string                 `json:"taskId,omitempty"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
	CreatedAt time.Time              `json:"createdAt"`
}

// EventBus is a simple in-memory broadcast bus.
// Design notes:
//   - multiple subscribers: each has an independent buffered channel, so a slow consumer does not block the publisher;
//   - drop when full: the publisher never blocks, avoiding stalling the listener accept loop / beacon handler;
//   - global filtering: subscribers can restrict by SessionID/Category, so the frontend subscribes on demand and saves CPU;
//   - safe close: after Close() all subscriber channels are closed, preventing goroutine leaks.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[string]*Subscription
	closed      bool
}

// Subscription is a subscription handle
type Subscription struct {
	ID         string
	Ch         chan *Event
	SessionID  string // empty means no restriction
	Category   string // empty means no restriction
	Levels     map[string]struct{}
	dropCount  atomic.Int64
}

// NewEventBus creates the bus
func NewEventBus() *EventBus {
	return &EventBus{subscribers: make(map[string]*Subscription)}
}

// Subscribe registers a subscriber; it returns a Subscription, and the caller is responsible for the later Unsubscribe.
//   - bufferSize: the per-subscriber channel capacity, recommended 64~256;
//   - sessionFilter / categoryFilter: an empty string = no restriction;
//   - levelFilter: something like []string{"warn","critical"}; nil/empty means receive all.
func (b *EventBus) Subscribe(id string, bufferSize int, sessionFilter, categoryFilter string, levelFilter []string) *Subscription {
	if bufferSize <= 0 {
		bufferSize = 128
	}
	sub := &Subscription{
		ID:        id,
		Ch:        make(chan *Event, bufferSize),
		SessionID: sessionFilter,
		Category:  categoryFilter,
	}
	if len(levelFilter) > 0 {
		sub.Levels = make(map[string]struct{}, len(levelFilter))
		for _, l := range levelFilter {
			sub.Levels[l] = struct{}{}
		}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(sub.Ch)
		return sub
	}
	b.subscribers[id] = sub
	return sub
}

// Unsubscribe deregisters a subscriber and closes the channel
func (b *EventBus) Unsubscribe(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if sub, ok := b.subscribers[id]; ok {
		delete(b.subscribers, id)
		close(sub.Ch)
	}
}

// Publish broadcasts an event to all subscribers; non-blocking, silently dropping when the channel is full
func (b *EventBus) Publish(e *Event) {
	if e == nil {
		return
	}
	b.mu.RLock()
	subs := make([]*Subscription, 0, len(b.subscribers))
	for _, s := range b.subscribers {
		if s.matches(e) {
			subs = append(subs, s)
		}
	}
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return
	}
	for _, s := range subs {
		select {
		case s.Ch <- e:
		default:
			s.dropCount.Add(1)
		}
	}
}

// Close closes the bus and stops all subscriptions
func (b *EventBus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, s := range b.subscribers {
		close(s.Ch)
		delete(b.subscribers, id)
	}
}

func (s *Subscription) matches(e *Event) bool {
	if s.SessionID != "" && e.SessionID != s.SessionID {
		return false
	}
	if s.Category != "" && e.Category != s.Category {
		return false
	}
	if len(s.Levels) > 0 {
		if _, ok := s.Levels[e.Level]; !ok {
			return false
		}
	}
	return true
}
