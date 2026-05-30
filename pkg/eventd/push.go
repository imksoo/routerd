// SPDX-License-Identifier: BSD-3-Clause

package eventd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/federation"
)

// httpDoer is the subset of *http.Client the push client uses, injectable so
// tests can wire the sender directly at an httptest.Server.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Pusher delivers federation events to configured peers with HMAC signing,
// per-peer filtering, idempotent enqueue, and bounded exponential backoff. The
// clock, http doer, and sleep func are injectable for deterministic tests.
type Pusher struct {
	store  DeliveryStore
	secret []byte
	peers  []PeerConfig
	retry  PushRetry

	client httpDoer
	now    func() time.Time
	sleep  func(time.Duration)
}

// NewPusher builds a Pusher. client/now/sleep may be nil for production defaults
// (http.DefaultClient with a timeout, time.Now, time.Sleep).
func NewPusher(store DeliveryStore, secret []byte, peers []PeerConfig, retry PushRetry, client httpDoer, now func() time.Time, sleep func(time.Duration)) *Pusher {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	if retry.MaxAttempts <= 0 {
		retry.MaxAttempts = DefaultMaxAttempts
	}
	if retry.BaseBackoff <= 0 {
		retry.BaseBackoff = DefaultBaseBackoff
	}
	if retry.MaxBackoff <= 0 {
		retry.MaxBackoff = DefaultMaxBackoff
	}
	return &Pusher{
		store:  store,
		secret: secret,
		peers:  peers,
		retry:  retry,
		client: client,
		now:    now,
		sleep:  sleep,
	}
}

// peerMatches reports whether ev should be delivered to peer per its filters:
// empty Types means all types; empty SubjectPrefixes means all subjects.
func peerMatches(peer PeerConfig, ev federation.Event) bool {
	if len(peer.Types) > 0 {
		ok := false
		for _, t := range peer.Types {
			if t == ev.Type {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(peer.SubjectPrefixes) > 0 {
		ok := false
		for _, p := range peer.SubjectPrefixes {
			if strings.HasPrefix(ev.Subject, p) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// PushEvent delivers ev to every matching peer, recording delivery state. It
// returns the first error encountered enqueuing/persisting (delivery failures
// are recorded as state.DeliveryFailed and do not return an error). ctx cancels
// in-flight retries.
func (p *Pusher) PushEvent(ctx context.Context, ev federation.Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	for _, peer := range p.peers {
		if !peerMatches(peer, ev) {
			continue
		}
		if err := p.deliverToPeer(ctx, peer, ev.ID, body); err != nil {
			return err
		}
	}
	return nil
}

// PushEventPending delivers ev to every matching peer EXCEPT those for which
// isDelivered(peer.NodeName) reports true (the event was already confirmed
// delivered to that peer). It is the outbox-loop entry point: the Outbox passes
// a closure backed by the delivery store so already-delivered (event,peer) pairs
// are not re-pushed and their attempt counters are not bumped. Delivery logic
// (filtering, enqueue, signed POST, backoff, status recording) is reused from
// the per-peer path. isDelivered may be nil (treat all peers as pending).
func (p *Pusher) PushEventPending(ctx context.Context, ev federation.Event, isDelivered func(peerNode string) bool) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	for _, peer := range p.peers {
		if !peerMatches(peer, ev) {
			continue
		}
		if isDelivered != nil && isDelivered(peer.NodeName) {
			continue
		}
		if err := p.deliverToPeer(ctx, peer, ev.ID, body); err != nil {
			return err
		}
	}
	return nil
}

// deliverToPeer enqueues then attempts delivery to a single peer with retries.
func (p *Pusher) deliverToPeer(ctx context.Context, peer PeerConfig, eventID string, body []byte) error {
	if err := p.store.RecordDelivery(eventID, peer.NodeName); err != nil {
		return fmt.Errorf("enqueue delivery %s -> %s: %w", eventID, peer.NodeName, err)
	}
	endpoint := strings.TrimRight(peer.Endpoint, "/") + "/v1/events"

	var lastErr string
	attempts := 0
	for attempt := 1; attempt <= p.retry.MaxAttempts; attempt++ {
		if ctx.Err() != nil {
			lastErr = ctx.Err().Error()
			break
		}
		attempts = attempt
		err := p.postOnce(ctx, endpoint, body)
		if err == nil {
			return p.store.UpdateDeliveryStatus(eventID, peer.NodeName, "delivered", attempts, "", p.now())
		}
		lastErr = err.Error()
		if attempt < p.retry.MaxAttempts {
			p.sleep(p.backoff(attempt))
		}
	}
	// Exhausted: record failure with zero delivered time.
	return p.store.UpdateDeliveryStatus(eventID, peer.NodeName, "failed", attempts, lastErr, time.Time{})
}

// postOnce signs and POSTs the body once, returning an error for transport
// failures or non-2xx responses.
func (p *Pusher) postOnce(ctx context.Context, endpoint string, body []byte) error {
	ts := p.now().Unix()
	sig := federation.Sign(p.secret, ts, body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderTimestamp, strconv.FormatInt(ts, 10))
	req.Header.Set(HeaderSignature, sig)
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("peer returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// backoff returns the exponential backoff for the given attempt (1-based),
// capped at MaxBackoff.
func (p *Pusher) backoff(attempt int) time.Duration {
	d := p.retry.BaseBackoff
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= p.retry.MaxBackoff {
			return p.retry.MaxBackoff
		}
	}
	if d > p.retry.MaxBackoff {
		d = p.retry.MaxBackoff
	}
	return d
}
