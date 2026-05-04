package bus

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

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
	logger      *slog.Logger
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

func (b *Bus) SetLogger(logger *slog.Logger) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.logger = logger
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
	logger := b.logger
	b.appendRecentLocked(event)
	var targets []chan Event
	for _, sub := range b.subscribers {
		if sub.matches(event) {
			targets = append(targets, sub.ch)
		}
	}
	b.mu.Unlock()
	logEvent(ctx, logger, event)

	for _, target := range targets {
		select {
		case target <- event:
		default:
			select {
			case target <- event:
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

func logEvent(ctx context.Context, logger *slog.Logger, event Event) {
	if logger == nil || event.Type == "" {
		return
	}
	level := slog.LevelInfo
	switch event.Severity {
	case daemonapi.SeverityDebug:
		level = slog.LevelDebug
	case daemonapi.SeverityWarning:
		level = slog.LevelWarn
	case daemonapi.SeverityError:
		level = slog.LevelError
	}
	if routineEvent(event.Type) {
		level = slog.LevelDebug
	}
	attrs := []slog.Attr{
		slog.String("topic", event.Type),
		slog.String("severity", event.Severity),
		slog.String("cursor", event.Cursor),
		slog.String("daemon.kind", event.Daemon.Kind),
		slog.String("daemon.name", event.Daemon.Name),
		slog.String("daemon.instance", event.Daemon.Instance),
	}
	if event.Resource != nil {
		attrs = append(attrs,
			slog.String("resource.apiVersion", event.Resource.APIVersion),
			slog.String("resource.kind", event.Resource.Kind),
			slog.String("resource.name", event.Resource.Name),
		)
	}
	if event.Reason != "" {
		attrs = append(attrs, slog.String("reason", event.Reason))
	}
	if len(event.Attributes) > 0 {
		attrs = append(attrs, slog.Any("attributes", event.Attributes))
	}
	logger.LogAttrs(ctx, level, "routerd event", attrs...)
}

func routineEvent(topic string) bool {
	switch topic {
	case "routerd.resource.status.changed",
		"routerd.daemon.resource.status",
		"routerd.lan.ipv4_address.applied",
		"routerd.lan.address.applied",
		"routerd.dns.resolver.configured",
		"routerd.ipv4.route.installed",
		"routerd.nat44.rule.applied",
		"routerd.tunnel.ds-lite.up",
		"routerd.dhcpv6.info.updated",
		"routerd.conntrack.snapshot":
		return true
	default:
		return false
	}
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
