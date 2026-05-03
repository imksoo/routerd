package bus

import (
	"context"
	"testing"
	"time"

	"routerd/pkg/daemonapi"
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
