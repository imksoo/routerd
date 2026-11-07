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

// Pusher delivers federation events to configured peers with HMAC signing,
// per-peer filtering, idempotent enqueue, and bounded exponential backoff.
type Pusher struct {
	store   DeliveryStore
	secret  []byte
	peers   []PeerConfig
	retry   PushRetry
	metrics *Metrics

	client *http.Client
}

// NewPusher builds a Pusher.
func NewPusher(store DeliveryStore, secret []byte, peers []PeerConfig, retry PushRetry) *Pusher {
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
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Pusher) SetMetrics(m *Metrics) { p.metrics = m }

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

// PushEventToPeer delivers ev to one peer. It is used by Outbox's per-peer
// drain workers so another peer's retry/backoff cannot delay this peer. The
// method filters, enqueues, retries, and records delivery metrics.
func (p *Pusher) PushEventToPeer(ctx context.Context, ev federation.Event, peer PeerConfig) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return p.pushEventToPeer(ctx, peer, ev, body)
}

func (p *Pusher) pushEventToPeer(ctx context.Context, peer PeerConfig, ev federation.Event, body []byte) error {
	if !peerMatches(peer, ev) {
		return nil
	}
	return p.deliverToPeer(ctx, peer, ev, body)
}

// deliverToPeer enqueues then attempts delivery to a single peer with retries.
func (p *Pusher) deliverToPeer(ctx context.Context, peer PeerConfig, ev federation.Event, body []byte) error {
	if err := p.store.RecordDelivery(ev.ID, peer.NodeName); err != nil {
		return fmt.Errorf("enqueue delivery %s -> %s: %w", ev.ID, peer.NodeName, err)
	}
	endpoint := strings.TrimRight(peer.Endpoint, "/") + "/v1/events"

	var lastErr string
	attempts := 0
retry:
	for attempt := 1; attempt <= p.retry.MaxAttempts; attempt++ {
		if ctx.Err() != nil {
			lastErr = ctx.Err().Error()
			break
		}
		attempts = attempt
		err := p.postOnce(ctx, endpoint, body)
		if err == nil {
			deliveredAt := time.Now()
			if storeErr := p.store.UpdateDeliveryStatus(ev.ID, peer.NodeName, "delivered", attempts, "", deliveredAt, ev.ExpiresAt); storeErr != nil {
				return storeErr
			}
			p.metrics.RecordDelivery(ctx, ev.Group, peer.NodeName, ev.Type, "delivered")
			p.metrics.RecordAttempts(ctx, ev.Group, peer.NodeName, ev.Type, "delivered", int64(attempts))
			if !ev.ObservedAt.IsZero() && !deliveredAt.IsZero() {
				lag := deliveredAt.Sub(ev.ObservedAt).Seconds()
				if lag >= 0 {
					p.metrics.RecordDeliveryLag(ctx, ev.Group, peer.NodeName, ev.Type, lag)
				}
			}
			return nil
		}
		lastErr = err.Error()
		if attempt < p.retry.MaxAttempts {
			timer := time.NewTimer(p.backoff(attempt))
			select {
			case <-ctx.Done():
				timer.Stop()
				lastErr = ctx.Err().Error()
				break retry
			case <-timer.C:
			}
		}
	}
	if storeErr := p.store.UpdateDeliveryStatus(ev.ID, peer.NodeName, "failed", attempts, lastErr, time.Time{}, ev.ExpiresAt); storeErr != nil {
		return storeErr
	}
	p.metrics.RecordDelivery(ctx, ev.Group, peer.NodeName, ev.Type, "failed")
	p.metrics.RecordAttempts(ctx, ev.Group, peer.NodeName, ev.Type, "failed", int64(attempts))
	return nil
}

// postOnce signs and POSTs the body once, returning an error for transport
// failures or non-2xx responses.
func (p *Pusher) postOnce(ctx context.Context, endpoint string, body []byte) error {
	ts := time.Now().Unix()
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
