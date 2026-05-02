package bus

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"routerd/pkg/daemonapi"
)

type Event = daemonapi.DaemonEvent

type Subscription struct {
	Topics   []string
	Resource *daemonapi.ResourceRef
	Source   *daemonapi.DaemonRef
	Filter   func(Event) bool
}

type Bus struct {
	mu          sync.Mutex
	nextCursor  uint64
	subscribers map[uint64]subscriber
	recent      map[string][]Event
	recentLimit int
	store       EventStore
}

type EventStore interface {
	RecordBusEvent(context.Context, Event) (string, error)
}

type subscriber struct {
	sub Subscription
	ch  chan Event
}

func New() *Bus {
	return &Bus{
		subscribers: map[uint64]subscriber{},
		recent:      map[string][]Event{},
		recentLimit: 200,
	}
}

func NewWithStore(store EventStore) *Bus {
	b := New()
	b.store = store
	return b
}

func (b *Bus) Publish(ctx context.Context, event Event) error {
	if b.store != nil {
		cursor, err := b.store.RecordBusEvent(ctx, event)
		if err != nil {
			return err
		}
		event.Cursor = cursor
	}
	b.mu.Lock()
	if event.Cursor == "" {
		b.nextCursor++
		event.Cursor = strconv.FormatUint(b.nextCursor, 10)
	}
	b.appendRecentLocked(event)
	var targets []chan Event
	for _, sub := range b.subscribers {
		if sub.matches(event) {
			targets = append(targets, sub.ch)
		}
	}
	b.mu.Unlock()

	for _, target := range targets {
		select {
		case target <- event:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (b *Bus) Subscribe(ctx context.Context, sub Subscription, buffer int) (<-chan Event, func()) {
	if buffer < 1 {
		buffer = 1
	}
	ch := make(chan Event, buffer)
	b.mu.Lock()
	b.nextCursor++
	id := b.nextCursor
	b.subscribers[id] = subscriber{sub: sub, ch: ch}
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, id)
			b.mu.Unlock()
			close(ch)
		})
	}
	if ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			cancel()
		}()
	}
	return ch, cancel
}

func (b *Bus) Recent(topic string) []Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	events := b.recent[topic]
	return append([]Event(nil), events...)
}

func (b *Bus) appendRecentLocked(event Event) {
	if event.Type == "" {
		return
	}
	events := append(b.recent[event.Type], event)
	if len(events) > b.recentLimit {
		events = append([]Event(nil), events[len(events)-b.recentLimit:]...)
	}
	b.recent[event.Type] = events
}

func (s subscriber) matches(event Event) bool {
	if len(s.sub.Topics) > 0 {
		matched := false
		for _, topic := range s.sub.Topics {
			if MatchTopic(topic, event.Type) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if s.sub.Resource != nil {
		if event.Resource == nil || *event.Resource != *s.sub.Resource {
			return false
		}
	}
	if s.sub.Source != nil && event.Daemon != *s.sub.Source {
		return false
	}
	if s.sub.Filter != nil && !s.sub.Filter(event) {
		return false
	}
	return true
}

func MatchTopic(pattern, topic string) bool {
	if pattern == "" {
		return topic == ""
	}
	return matchSegments(strings.Split(pattern, "."), strings.Split(topic, "."))
}

func matchSegments(pattern, topic []string) bool {
	if len(pattern) == 0 {
		return len(topic) == 0
	}
	if pattern[0] == "**" {
		if matchSegments(pattern[1:], topic) {
			return true
		}
		return len(topic) > 0 && matchSegments(pattern, topic[1:])
	}
	if len(topic) == 0 {
		return false
	}
	if pattern[0] != "*" && pattern[0] != topic[0] {
		return false
	}
	return matchSegments(pattern[1:], topic[1:])
}
