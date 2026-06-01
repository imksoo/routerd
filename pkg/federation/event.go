// SPDX-License-Identifier: BSD-3-Clause

// Package federation holds the external CloudEdge Event Federation envelope and
// helpers (ADR 0006).
//
// This is the cross-node event bus type. It is deliberately separate from the
// observability event store (pkg/eventlog, pkg/eventfile) and from the node-local
// EventRule automation primitive (pkg/eventrule): those record/act on local
// telemetry, whereas a federation Event is propagated between routerd nodes and
// carries an observed fact, never a command to execute.
package federation

import (
	"fmt"
	"strings"
	"time"
)

const (
	// ObservedIPv4EventType is the data-plane observation topic used by
	// CloudEdge Mobility lease projection.
	ObservedIPv4EventType = "routerd.client.ipv4.observed"

	// MobilityMemberHeartbeatType is the control-plane liveness topic emitted
	// by MobilityPool auto-failover members.
	MobilityMemberHeartbeatType = "routerd.mobility.member.heartbeat"
)

// Event is the external Event Federation envelope exchanged between routerd
// nodes. It is an observed fact (e.g. "routerd.client.ipv4.observed"), not
// config and not a command.
type Event struct {
	// ID is the store idempotency key. A duplicate ID is a no-op insert
	// (the store enforces uniqueness on ID), matching at-least-once delivery.
	ID string `json:"id" yaml:"id"`
	// Group is the EventGroup (bus) the event belongs to.
	Group string `json:"group" yaml:"group"`
	// SourceNode is the emitting node's identity (EventGroup.nodeName).
	SourceNode string `json:"sourceNode,omitempty" yaml:"sourceNode,omitempty"`
	// Type is the typed topic, e.g. "routerd.client.ipv4.observed".
	Type string `json:"type" yaml:"type"`
	// Subject is the entity the event is about, e.g. "10.88.60.9/32".
	Subject string `json:"subject,omitempty" yaml:"subject,omitempty"`
	// DedupeKey is a stable grouping key that subscriptions MAY use to collapse
	// repeated observations of the same fact. It defaults to ID when empty.
	// Unlike ID, the store does NOT enforce uniqueness on DedupeKey in Phase 1:
	// idempotency is keyed on ID; DedupeKey is a subscription-side hint only.
	DedupeKey string `json:"dedupeKey,omitempty" yaml:"dedupeKey,omitempty"`
	// Payload carries additional typed-but-stringly attributes.
	Payload map[string]string `json:"payload,omitempty" yaml:"payload,omitempty"`
	// ObservedAt is when the fact was observed at the source.
	ObservedAt time.Time `json:"observedAt,omitempty" yaml:"observedAt,omitempty"`
	// ExpiresAt is when the event becomes stale; zero means no expiry.
	ExpiresAt time.Time `json:"expiresAt,omitempty" yaml:"expiresAt,omitempty"`
}

// Normalize fills derived defaults (DedupeKey from ID) and validates the
// required fields. It does NOT consult the wall clock; callers stamp times.
func (e *Event) Normalize() error {
	e.ID = strings.TrimSpace(e.ID)
	e.Group = strings.TrimSpace(e.Group)
	e.Type = strings.TrimSpace(e.Type)
	e.SourceNode = strings.TrimSpace(e.SourceNode)
	e.Subject = strings.TrimSpace(e.Subject)
	e.DedupeKey = strings.TrimSpace(e.DedupeKey)
	if e.DedupeKey == "" {
		e.DedupeKey = e.ID
	}
	return e.Validate()
}

// Validate reports whether the required envelope fields are present.
func (e Event) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return fmt.Errorf("federation event id is required")
	}
	if strings.TrimSpace(e.Group) == "" {
		return fmt.Errorf("federation event group is required")
	}
	if strings.TrimSpace(e.Type) == "" {
		return fmt.Errorf("federation event type is required")
	}
	return nil
}

// IsExpired reports whether the event has passed its ExpiresAt as of now. A zero
// ExpiresAt never expires. now is passed in so this helper stays pure.
func (e Event) IsExpired(now time.Time) bool {
	if e.ExpiresAt.IsZero() {
		return false
	}
	return now.After(e.ExpiresAt)
}

// IsControlPlaneLivenessType reports whether typ is a control-plane liveness
// topic. These events must not be scoped by data-plane subject prefixes.
func IsControlPlaneLivenessType(typ string) bool {
	return strings.TrimSpace(typ) == MobilityMemberHeartbeatType
}
