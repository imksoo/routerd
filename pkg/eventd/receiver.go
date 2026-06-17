// SPDX-License-Identifier: BSD-3-Clause

package eventd

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/imksoo/routerd/pkg/federation"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// Header names for the signed push protocol (ADR 0006 Phase 2).
const (
	HeaderTimestamp = "X-Routerd-Timestamp"
	HeaderSignature = "X-Routerd-Signature"
)

// maxBodyBytes caps the receive body to a sane size for a single event.
const maxBodyBytes = 1 << 20

// Receiver is the HTTP handler that accepts signed federation events from peers
// and persists them idempotently. It is mountable on any http.ServeMux and so
// is exercisable via httptest without a real socket.
type Receiver struct {
	store        EventStore
	secret       []byte
	group        string
	node         string
	replayWindow time.Duration
	now          func() time.Time
	metrics      *Metrics

	received  atomic.Uint64
	rejected  atomic.Uint64
	duplicate atomic.Uint64
}

// NewReceiver builds a Receiver. now may be nil to use time.Now.
func NewReceiver(store EventStore, secret []byte, group, node string, replayWindow time.Duration, now func() time.Time) *Receiver {
	if now == nil {
		now = time.Now
	}
	return &Receiver{
		store:        store,
		secret:       secret,
		group:        group,
		node:         node,
		replayWindow: replayWindow,
		now:          now,
	}
}

func (r *Receiver) SetMetrics(m *Metrics) { r.metrics = m }

// Register mounts the receiver routes on mux.
func (r *Receiver) Register(mux *http.ServeMux) {
	mux.HandleFunc("/v1/events", r.handleEvents)
	mux.HandleFunc("/v1/status", r.handleStatus)
}

// Handler returns an http.Handler with the receiver routes mounted.
func (r *Receiver) Handler() http.Handler {
	mux := http.NewServeMux()
	r.Register(mux)
	return mux
}

func (r *Receiver) handleEvents(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	if req.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(req.Body, maxBodyBytes))
	if err != nil {
		r.rejected.Add(1)
		r.metrics.RecordReceiverReject(ctx, r.group, "read_body")
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	tsRaw := req.Header.Get(HeaderTimestamp)
	sig := req.Header.Get(HeaderSignature)
	ts, err := strconv.ParseInt(tsRaw, 10, 64)
	if err != nil {
		r.rejected.Add(1)
		r.metrics.RecordReceiverReject(ctx, r.group, "bad_timestamp")
		http.Error(w, "bad timestamp", http.StatusBadRequest)
		return
	}
	if err := federation.Verify(r.secret, ts, body, sig, r.now(), r.replayWindow); err != nil {
		r.rejected.Add(1)
		switch err {
		case federation.ErrStaleTimestamp:
			r.metrics.RecordReceiverReject(ctx, r.group, "stale_timestamp")
			http.Error(w, "stale timestamp", http.StatusForbidden)
		default:
			r.metrics.RecordReceiverReject(ctx, r.group, "bad_signature")
			http.Error(w, "bad signature", http.StatusUnauthorized)
		}
		return
	}

	var ev federation.Event
	if err := json.Unmarshal(body, &ev); err != nil {
		r.rejected.Add(1)
		r.metrics.RecordReceiverReject(ctx, r.group, "bad_body")
		http.Error(w, "bad event body", http.StatusBadRequest)
		return
	}
	if err := ev.Normalize(); err != nil {
		r.rejected.Add(1)
		r.metrics.RecordReceiverReject(ctx, r.group, "validation")
		http.Error(w, "invalid event: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Idempotency: a duplicate ID is a no-op insert, so detect duplicates for
	// the metric/log without breaking the at-least-once 200 contract.
	existing, _ := r.store.ListFederationEvents(ev.Group, true, r.now().Unix())
	dup := false
	for _, e := range existing {
		if e.ID == ev.ID {
			dup = true
			break
		}
	}

	rec := routerstate.EventRecord{
		ID:         ev.ID,
		Group:      ev.Group,
		SourceNode: ev.SourceNode,
		Type:       ev.Type,
		Subject:    ev.Subject,
		DedupeKey:  ev.DedupeKey,
		Payload:    ev.Payload,
		ObservedAt: ev.ObservedAt,
		ExpiresAt:  ev.ExpiresAt,
	}
	if err := r.store.RecordFederationEvent(rec); err != nil {
		// Persistence failure is the one case we must NOT 200 on: the peer
		// should retry. Do not count it as received.
		r.rejected.Add(1)
		r.metrics.RecordReceiverReject(ctx, r.group, "store_error")
		http.Error(w, "store error", http.StatusInternalServerError)
		return
	}
	if dup {
		r.duplicate.Add(1)
		r.metrics.RecordReceiverDuplicate(ctx, r.group)
	} else {
		r.received.Add(1)
		r.metrics.RecordReceiverAccepted(ctx, r.group)
	}
	// 200 even on duplicate id — at-least-once.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"accepted":  true,
		"id":        ev.ID,
		"duplicate": dup,
	})
}

// Status is the small JSON returned by GET /v1/status.
type Status struct {
	Node            string `json:"node"`
	Group           string `json:"group"`
	Received        uint64 `json:"received"`
	Duplicate       uint64 `json:"duplicate"`
	Rejected        uint64 `json:"rejected"`
	StoredEvents    int    `json:"storedEvents"`
	ReplayWindowSec int    `json:"replayWindowSeconds"`
}

func (r *Receiver) handleStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(r.Status())
}

// Status returns a snapshot of receiver counters and the stored event count.
func (r *Receiver) Status() Status {
	stored, _ := r.store.ListFederationEvents(r.group, true, r.now().Unix())
	return Status{
		Node:            r.node,
		Group:           r.group,
		Received:        r.received.Load(),
		Duplicate:       r.duplicate.Load(),
		Rejected:        r.rejected.Load(),
		StoredEvents:    len(stored),
		ReplayWindowSec: int(r.replayWindow / time.Second),
	}
}
