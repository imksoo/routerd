// SPDX-License-Identifier: BSD-3-Clause

package bus

import (
	"context"
	"testing"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

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
