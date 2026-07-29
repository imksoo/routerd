// SPDX-License-Identifier: BSD-3-Clause

package eventconsumer

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const pageSize = 256

type Store interface {
	ListEvents(query routerstate.EventQuery) ([]routerstate.StoredEvent, error)
	LoadOrInitializeEventConsumerCursor(consumer string) (int64, error)
	SaveEventConsumerCursor(consumer string, cursor int64) error
}

// Drain processes stored events in cursor order. The cursor advances only
// after process succeeds, so a store or consumer failure is retried on the
// next wake-up or periodic poll.
func Drain(ctx context.Context, store Store, consumer string, process func(context.Context, daemonapi.DaemonEvent) error) error {
	cursor, err := store.LoadOrInitializeEventConsumerCursor(consumer)
	if err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		events, err := store.ListEvents(routerstate.EventQuery{
			Limit:     pageSize,
			SinceID:   cursor,
			Ascending: true,
		})
		if err != nil {
			return err
		}
		for _, stored := range events {
			if err := process(ctx, Event(stored)); err != nil {
				return err
			}
			if err := store.SaveEventConsumerCursor(consumer, stored.ID); err != nil {
				return err
			}
			cursor = stored.ID
		}
		if len(events) < pageSize {
			return nil
		}
	}
}

// Backoff suppresses drain attempts after a consumer failure. It is intended
// to be owned by one consumer loop.
type Backoff struct {
	Initial time.Duration
	Max     time.Duration
	Now     func() time.Time

	failures int
	retryAt  time.Time
}

func (b *Backoff) Ready() bool {
	if b.retryAt.IsZero() {
		return true
	}
	return !b.now().Before(b.retryAt)
}

func (b *Backoff) Success() {
	b.failures = 0
	b.retryAt = time.Time{}
}

func (b *Backoff) Failure() {
	initial := b.Initial
	if initial <= 0 {
		initial = time.Second
	}
	maximum := b.Max
	if maximum <= 0 {
		maximum = time.Minute
	}
	delay := initial
	for i := 0; i < b.failures && delay < maximum; i++ {
		if delay > maximum/2 {
			delay = maximum
			break
		}
		delay *= 2
	}
	if delay > maximum {
		delay = maximum
	}
	b.failures++
	b.retryAt = b.now().Add(delay)
}

func (b *Backoff) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func Event(stored routerstate.StoredEvent) daemonapi.DaemonEvent {
	topic := stored.Topic
	if topic == "" {
		topic = stored.Type
	}
	event := daemonapi.DaemonEvent{
		Cursor:   strconv.FormatInt(stored.ID, 10),
		Time:     stored.CreatedAt,
		Type:     topic,
		Severity: stored.Severity,
		Reason:   stored.Reason,
		Message:  stored.Message,
		Daemon: daemonapi.DaemonRef{
			Kind:     stored.SourceKind,
			Instance: stored.SourceInstance,
		},
	}
	if stored.ResourceKind != "" || stored.ResourceName != "" {
		event.Resource = &daemonapi.ResourceRef{
			APIVersion: stored.ResourceAPIVersion,
			Kind:       stored.ResourceKind,
			Name:       stored.ResourceName,
		}
	}
	if len(stored.Attributes) > 0 {
		event.Attributes = make(map[string]string, len(stored.Attributes))
		for key, value := range stored.Attributes {
			event.Attributes[key] = fmt.Sprint(value)
		}
	}
	return event
}
