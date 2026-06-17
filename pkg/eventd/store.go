// SPDX-License-Identifier: BSD-3-Clause

package eventd

import (
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

// EventStore is the persistence surface the receiver needs: record an incoming
// federation event idempotently and list events (used by /v1/status counts and
// the prune loop). *state.SQLiteStore satisfies this.
type EventStore interface {
	RecordFederationEvent(rec routerstate.EventRecord) error
	ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error)
	PruneFederationEvents(group string, maxAge time.Duration, maxEvents int, now time.Time) (int64, error)
}

// DeliveryStore is the persistence surface the push client needs to track
// per-(event,peer) delivery attempts. *state.SQLiteStore satisfies this.
type DeliveryStore interface {
	RecordDelivery(eventID, peer string) error
	UpdateDeliveryStatus(eventID, peer, status string, attempts int, lastErr string, deliveredAt time.Time, eventExpiresAt time.Time) error
	ListDeliveries(eventID, peer string) ([]routerstate.DeliveryRecord, error)
}
