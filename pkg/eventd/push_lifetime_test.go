// SPDX-License-Identifier: BSD-3-Clause

package eventd_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/eventd"
)

func TestPushCancellationInterruptsBackoff(t *testing.T) {
	store := openStore(t, "cancel-push.db")
	now := time.Now().UTC()
	ev := seedEvent(t, store, "cancel-push", "group", "observed", "address", now)
	first := make(chan struct{})
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(first)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	pusher := eventd.NewPusher(store, testSecret, []eventd.PeerConfig{{NodeName: "peer", Endpoint: srv.URL}}, eventd.PushRetry{MaxAttempts: 3, BaseBackoff: time.Hour, MaxBackoff: time.Hour}, srv.Client(), time.Now, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- pusher.PushEvent(ctx, ev) }()
	<-first
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled delivery returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("delivery stayed in backoff after cancellation")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests=%d", got)
	}
	rows, err := store.ListDeliveries(ev.ID, "peer")
	if err != nil || len(rows) != 1 || rows[0].Status != "failed" || rows[0].Attempts != 1 {
		t.Fatalf("delivery result=%+v err=%v", rows, err)
	}
}

func TestPushRechecksExpiryAfterBackoffAndBeforeNextPeer(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(map[bool]string{false: "next-peer", true: "retry"}[retry], func(t *testing.T) {
			store := openStore(t, "expiry.db")
			now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
			ev := seedEvent(t, store, "expiry", "group", "observed", "address", now)
			ev.ExpiresAt = now.Add(time.Second)
			var requests atomic.Int32
			var clock atomic.Int64
			clock.Store(now.UnixNano())
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if retry {
					w.WriteHeader(http.StatusServiceUnavailable)
				} else {
					clock.Store(ev.ExpiresAt.UnixNano())
				}
			}))
			defer srv.Close()
			peers := []eventd.PeerConfig{{NodeName: "one", Endpoint: srv.URL}}
			if !retry {
				peers = append(peers, eventd.PeerConfig{NodeName: "two", Endpoint: srv.URL})
			}
			pusher := eventd.NewPusher(store, testSecret, peers, eventd.PushRetry{MaxAttempts: 3}, srv.Client(), func() time.Time { return time.Unix(0, clock.Load()) }, func(context.Context, time.Duration) error { clock.Store(ev.ExpiresAt.UnixNano()); return nil })
			if err := pusher.PushEvent(context.Background(), ev); err != nil {
				t.Fatal(err)
			}
			if got := requests.Load(); got != 1 {
				t.Fatalf("expired event sent %d times", got)
			}
			peer := "one"
			attempts := 1
			if !retry {
				peer = "two"
				attempts = 0
			}
			rows, err := store.ListDeliveries(ev.ID, peer)
			if err != nil || len(rows) != 1 || rows[0].Status != "failed" || rows[0].Attempts != attempts || rows[0].LastError != "event expired before delivery" {
				t.Fatalf("delivery result=%+v err=%v", rows, err)
			}
		})
	}
}
