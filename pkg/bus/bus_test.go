// SPDX-License-Identifier: BSD-3-Clause

package bus

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestPublishSubscribeWithTopicGlobAndResource(t *testing.T) {
	b := New()
	resource := daemonapi.ResourceRef{APIVersion: "net.routerd.net/v1alpha1", Kind: "DHCPv6PrefixDelegation", Name: "wan-pd"}
	ch, cancel := b.Subscribe(context.Background(), Subscription{
		Topics:   []string{"routerd.dhcpv6.client.prefix.*"},
		Resource: &resource,
	}, 2)
	defer cancel()

	if err := b.Publish(context.Background(), daemonapi.DaemonEvent{
		Daemon:   daemonapi.DaemonRef{Name: "wan-pd", Kind: "routerd-dhcpv6-client"},
		Resource: &resource,
		Type:     daemonapi.EventDHCPv6PrefixBound,
		Severity: daemonapi.SeverityInfo,
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-ch:
		if event.Cursor == "" {
			t.Fatal("cursor was not assigned")
		}
		if event.Type != daemonapi.EventDHCPv6PrefixBound {
			t.Fatalf("event type = %q", event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestSlowSubscriberDoesNotBlockBus(t *testing.T) {
	b := New()
	var logs bytes.Buffer
	b.SetLogger(slog.New(slog.NewTextHandler(&logs, nil)))
	ch, cancel := b.Subscribe(context.Background(), Subscription{
		Topics: []string{"routerd.**"},
	}, 1)
	defer cancel()

	started := time.Now()
	for range 100 {
		if err := b.Publish(context.Background(), daemonapi.DaemonEvent{
			Type: "routerd.test.event",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("publishing to a slow subscriber took %s", elapsed)
	}

	done := make(chan struct{})
	go func() {
		_, nextCancel := b.Subscribe(context.Background(), Subscription{}, 1)
		nextCancel()
		b.Recent("routerd.test.event")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("slow subscriber blocked bus operations")
	}
	if !strings.Contains(logs.String(), "event dropped for slow subscriber") ||
		!strings.Contains(logs.String(), "subscriber=") {
		t.Fatalf("drop was not logged with subscriber identity: %s", logs.String())
	}

	// Keep the subscription live until after the concurrency assertion.
	_ = ch
}

func TestCancelClosesSubscriptionDuringPublish(t *testing.T) {
	b := New()
	ch, cancel := b.Subscribe(context.Background(), Subscription{}, 1)
	for range 10 {
		if err := b.Publish(context.Background(), daemonapi.DaemonEvent{Type: "routerd.test.event"}); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	for range ch {
	}
}

func TestMatchTopic(t *testing.T) {
	tests := []struct {
		pattern string
		topic   string
		want    bool
	}{
		{"routerd.dhcpv6.client.prefix.*", "routerd.dhcpv6.client.prefix.bound", true},
		{"routerd.dhcpv6.**", "routerd.dhcpv6.client.prefix.bound", true},
		{"routerd.*.client", "routerd.dhcpv6.client", true},
		{"routerd.*.client", "routerd.dhcpv6.client.prefix", false},
		{"routerd.daemon.**", "routerd.dhcpv6.client.prefix.bound", false},
	}
	for _, tt := range tests {
		if got := MatchTopic(tt.pattern, tt.topic); got != tt.want {
			t.Fatalf("MatchTopic(%q, %q) = %v, want %v", tt.pattern, tt.topic, got, tt.want)
		}
	}
}
