// SPDX-License-Identifier: BSD-3-Clause

package eventd

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/federation"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// DefaultMaxConcurrentPeerDrains bounds the number of peers whose retry loop
// may be active in one outbox pass. Each peer still has exactly one sequential
// drain, so events retain their per-peer order while an unreachable peer cannot
// hold up a healthy one.
const DefaultMaxConcurrentPeerDrains = 4

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
	metrics    *Metrics

	maxConcurrentPeerDrains int
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
		events:                  events,
		deliveries:              deliveries,
		pusher:                  pusher,
		group:                   group,
		nodeName:                nodeName,
		interval:                interval,
		now:                     now,
		maxConcurrentPeerDrains: DefaultMaxConcurrentPeerDrains,
	}
}

func (o *Outbox) SetMetrics(m *Metrics) { o.metrics = m }

// SetMaxConcurrentPeerDrains changes the bounded per-pass peer parallelism.
// Call it before Run or RunOnce. Values less than one restore the safe default.
func (o *Outbox) SetMaxConcurrentPeerDrains(max int) {
	if max < 1 {
		max = DefaultMaxConcurrentPeerDrains
	}
	o.maxConcurrentPeerDrains = max
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
// and push each locally-originated, not-yet-delivered (event,peer) pair. Peer
// drains are isolated from one another and bounded, but events for one peer
// remain sequential in the store's observed-at order.
func (o *Outbox) RunOnce(ctx context.Context) error {
	start := o.now()
	err := o.runOnceInner(ctx)
	elapsed := o.now().Sub(start).Seconds()
	o.metrics.RecordOutboxTick(ctx, o.group)
	o.metrics.RecordOutboxTickDuration(ctx, o.group, elapsed)
	if err != nil {
		o.metrics.RecordOutboxTickError(ctx, o.group)
	}
	return err
}

func (o *Outbox) runOnceInner(ctx context.Context) error {
	records, err := o.events.ListFederationEvents(o.group, false, o.now().Unix())
	if err != nil {
		return err
	}
	events := make([]outboxEvent, 0, len(records))
	for _, rec := range records {
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
		delivered, err := o.deliveredPeers(ctx, rec.ID, rec.Type, rec.ExpiresAt)
		if err != nil {
			return err
		}
		events = append(events, outboxEvent{event: ev, delivered: delivered})
	}
	return o.drainPeers(ctx, events)
}

type outboxEvent struct {
	event     federation.Event
	delivered map[string]bool
}

type peerDrain struct {
	peer PeerConfig
}

// drainPeers runs no more than maxConcurrentPeerDrains peer retry loops at a
// time. A single loop owns one peer's event sequence, which both prevents a
// failing peer from becoming a global head-of-line blocker and preserves the
// ordering of attempts to that peer.
func (o *Outbox) drainPeers(ctx context.Context, events []outboxEvent) error {
	if o.pusher == nil || len(events) == 0 {
		return nil
	}
	jobs := make([]peerDrain, 0, len(o.pusher.peers))
	for _, peer := range o.pusher.peers {
		if peerHasPendingEvents(peer, events) {
			jobs = append(jobs, peerDrain{peer: peer})
		}
	}
	if len(jobs) == 0 {
		return nil
	}

	workers := o.maxConcurrentPeerDrains
	if workers < 1 {
		workers = DefaultMaxConcurrentPeerDrains
	}
	if workers > len(jobs) {
		workers = len(jobs)
	}

	jobCh := make(chan peerDrain)
	errs := make(chan error, len(jobs))
	var workersDone sync.WaitGroup
	for range workers {
		workersDone.Add(1)
		go func() {
			defer workersDone.Done()
			for job := range jobCh {
				if err := o.drainPeer(ctx, job.peer, events); err != nil {
					errs <- err
				}
			}
		}()
	}

	var dispatchErr error
dispatch:
	for _, job := range jobs {
		select {
		case jobCh <- job:
		case <-ctx.Done():
			dispatchErr = ctx.Err()
			break dispatch
		}
	}
	close(jobCh)
	workersDone.Wait()
	close(errs)
	for err := range errs {
		dispatchErr = errors.Join(dispatchErr, err)
	}
	return dispatchErr
}

func peerHasPendingEvents(peer PeerConfig, events []outboxEvent) bool {
	for _, item := range events {
		if peerMatches(peer, item.event) && !item.delivered[peer.NodeName] {
			return true
		}
	}
	return false
}

func (o *Outbox) drainPeer(ctx context.Context, peer PeerConfig, events []outboxEvent) error {
	for _, item := range events {
		if !peerMatches(peer, item.event) || item.delivered[peer.NodeName] {
			continue
		}
		if err := o.pusher.PushEventToPeer(ctx, item.event, peer); err != nil {
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
func (o *Outbox) deliveredPeers(ctx context.Context, eventID, eventType string, eventExpiresAt time.Time) (map[string]bool, error) {
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
			o.metrics.RecordStaleTTL(ctx, o.group, row.Peer, eventType)
			o.metrics.RecordRepush(ctx, o.group, row.Peer, eventType)
			continue
		}
		set[row.Peer] = true
	}
	return set, nil
}
