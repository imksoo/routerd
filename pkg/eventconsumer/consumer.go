// SPDX-License-Identifier: BSD-3-Clause

package eventconsumer

import (
	"context"
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
// Processing is at least once: successful processing followed by a cursor
// write failure repeats the same event on retry, including after restart.
// Side effects must use durable idempotency/observation; cursor persistence is
// not atomic with an external operation. A newly initialized consumer starts
// at the current journal tip, rather than replaying preexisting history.
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
	delay   time.Duration
	retryAt time.Time
}

func (b *Backoff) Ready() bool {
	return !time.Now().Before(b.retryAt)
}

func (b *Backoff) Success() {
	b.delay = 0
	b.retryAt = time.Time{}
}

func (b *Backoff) Failure() {
	if b.delay == 0 {
		b.delay = time.Second
	} else {
		b.delay = min(2*b.delay, time.Minute)
	}
	b.retryAt = time.Now().Add(b.delay)
}

func Event(stored routerstate.StoredEvent) daemonapi.DaemonEvent {
	topic := stored.Topic
	if topic == "" {
		topic = stored.Type
	}
	event := daemonapi.DaemonEvent{
		Cursor:     strconv.FormatInt(stored.ID, 10),
		Time:       stored.CreatedAt,
		Type:       topic,
		Severity:   stored.Severity,
		Reason:     stored.Reason,
		Message:    stored.Message,
		Attributes: stored.Attributes,
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
	return event
}
