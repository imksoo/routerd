// SPDX-License-Identifier: BSD-3-Clause

package eventd

import (
	"context"
	"time"

	"github.com/imksoo/routerd/pkg/federation"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// Outbox drains locally-originated federation events to peers. The receiver
// persists events to the shared store; routerctl emit writes local events to
// the same store; the Outbox is what actually pushes those local events to
// peers (the missing half that Pusher alone never drove). It is idempotent and
// safe to run on a ticker: already-delivered (event,peer) pairs are skipped and
// pending/failed pairs are re-attempted each pass, which yields the
// restart/peer-recovery resend property.
type Outbox struct {
	events     EventStore
	deliveries DeliveryStore
	pusher     *Pusher
	group      string
	nodeName   string
	interval   time.Duration
	now        func() time.Time
}

// NewOutbox builds an Outbox. now may be nil to use time.Now. interval <= 0
// falls back to DefaultPushInterval.
func NewOutbox(events EventStore, deliveries DeliveryStore, pusher *Pusher, group, nodeName string, interval time.Duration, now func() time.Time) *Outbox {
	if now == nil {
		now = time.Now
	}
	if interval <= 0 {
		interval = DefaultPushInterval
	}
	return &Outbox{
		events:     events,
		deliveries: deliveries,
		pusher:     pusher,
		group:      group,
		nodeName:   nodeName,
		interval:   interval,
		now:        now,
	}
}

// Run drains once immediately, then on every interval tick until ctx is done.
// onError, when non-nil, is invoked with any drain error. Mirrors Pruner.Run.
func (o *Outbox) Run(ctx context.Context, onError func(error)) {
	if err := o.RunOnce(ctx); err != nil && onError != nil {
		onError(err)
	}
	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := o.RunOnce(ctx); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}

// RunOnce performs a single drain pass: list non-expired events for the group
// and push each locally-originated, not-yet-delivered (event,peer) pair.
func (o *Outbox) RunOnce(ctx context.Context) error {
	events, err := o.events.ListFederationEvents(o.group, false, o.now().Unix())
	if err != nil {
		return err
	}
	for _, rec := range events {
		// Loop prevention (ADR 0006 invariant): only push events that
		// originated on THIS node. Events received FROM peers carry a
		// different SourceNode and must NEVER be re-emitted, otherwise a
		// federated event would ping-pong around the mesh.
		if rec.SourceNode != o.nodeName {
			continue
		}
		ev := federation.Event{
			ID:         rec.ID,
			Group:      rec.Group,
			SourceNode: rec.SourceNode,
			Type:       rec.Type,
			Subject:    rec.Subject,
			DedupeKey:  rec.DedupeKey,
			Payload:    rec.Payload,
			ObservedAt: rec.ObservedAt,
			ExpiresAt:  rec.ExpiresAt,
		}
		delivered, err := o.deliveredPeers(rec.ID, rec.ExpiresAt)
		if err != nil {
			return err
		}
		if err := o.pusher.PushEventPending(ctx, ev, func(peer string) bool {
			return delivered[peer]
		}); err != nil {
			return err
		}
	}
	return nil
}

// deliveredPeers returns the set of peer node names whose latest delivery row
// for eventID is in status delivered AND whose recorded event_expires_at matches
// the event's current ExpiresAt. When the event's TTL has been refreshed
// (ExpiresAt moved forward since the last push), the peer is treated as
// not-yet-delivered so the outbox re-pushes the refreshed event.
func (o *Outbox) deliveredPeers(eventID string, eventExpiresAt time.Time) (map[string]bool, error) {
	rows, err := o.deliveries.ListDeliveries(eventID, "")
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.Status != routerstate.DeliveryDelivered {
			continue
		}
		if !eventExpiresAt.IsZero() && row.EventExpiresAt.Before(eventExpiresAt) {
			continue
		}
		set[row.Peer] = true
	}
	return set, nil
}
