// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type fakeShardStore struct {
	events   []routerstate.EventRecord
	statuses map[string]map[string]any
}

func newFakeShardStore() *fakeShardStore {
	return &fakeShardStore{statuses: map[string]map[string]any{}}
}

func (s *fakeShardStore) RecordFederationEvent(rec routerstate.EventRecord) error {
	for i, existing := range s.events {
		if existing.ID == rec.ID {
			s.events[i] = rec
			return nil
		}
	}
	s.events = append(s.events, rec)
	return nil
}

func (s *fakeShardStore) ListFederationEvents(group string, includeExpired bool, now int64) ([]routerstate.EventRecord, error) {
	var out []routerstate.EventRecord
	for _, ev := range s.events {
		if ev.Group != group {
			continue
		}
		if !includeExpired && !ev.ExpiresAt.IsZero() && ev.ExpiresAt.Unix() < now {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}

func (s *fakeShardStore) MergeObjectStatus(apiVersion, kind, name string, updates map[string]any) error {
	key := apiVersion + "/" + kind + "/" + name
	if s.statuses[key] == nil {
		s.statuses[key] = map[string]any{}
	}
	for k, v := range updates {
		s.statuses[key][k] = v
	}
	return nil
}

func TestShardControllerEmitsEvents(t *testing.T) {
	store := newFakeShardStore()
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	router := &api.Router{
		Spec: api.RouterSpec{
			Resources: []api.Resource{
				{
					TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
					Metadata: api.ObjectMeta{Name: "cloudedge"},
					Spec:     api.EventGroupSpec{NodeName: "rr-node"},
				},
				{
					TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMSubnetPolicy"},
					Metadata: api.ObjectMeta{Name: "office-10-net"},
					Spec: api.SAMSubnetPolicySpec{
						SourcePrefix: "10.0.0.0/8",
						PoolRef:      "cloudedge",
						GroupRef:     "cloudedge",
						Shards: []api.SAMSubnetShard{
							{Prefix: "10.0.1.0/25", AssignedNodes: []string{"oci-a", "oci-b"}},
							{Prefix: "10.0.2.0/25", AssignedNodes: []string{"aws-a"}},
						},
					},
				},
			},
		},
	}
	ctrl := ShardController{Router: router, Store: store, Now: func() time.Time { return now }}
	if err := ctrl.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(store.events))
	}
	pools := map[string][]string{}
	for _, ev := range store.events {
		if ev.Type != ShardAssignedEventType {
			t.Fatalf("unexpected event type %q", ev.Type)
		}
		if ev.Group != "cloudedge" {
			t.Fatalf("unexpected group %q", ev.Group)
		}
		pools[ev.Payload["prefix"]] = append(pools[ev.Payload["prefix"]], ev.Payload["node"])
	}
	if len(pools["10.0.1.0/25"]) != 2 {
		t.Fatalf("expected 2 nodes for 10.0.1.0/25, got %d", len(pools["10.0.1.0/25"]))
	}
	if len(pools["10.0.2.0/25"]) != 1 {
		t.Fatalf("expected 1 node for 10.0.2.0/25, got %d", len(pools["10.0.2.0/25"]))
	}
}

func TestResolveShardScopeReturnsAssignedPrefixes(t *testing.T) {
	store := newFakeShardStore()
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	store.events = []routerstate.EventRecord{
		{
			ID: "shard-1", Group: "cloudedge", Type: ShardAssignedEventType,
			Payload:   map[string]string{"pool": "cloudedge", "prefix": "10.0.1.0/25", "node": "oci-a"},
			ExpiresAt: now.Add(10 * time.Minute),
		},
		{
			ID: "shard-2", Group: "cloudedge", Type: ShardAssignedEventType,
			Payload:   map[string]string{"pool": "cloudedge", "prefix": "10.0.2.0/25", "node": "oci-a"},
			ExpiresAt: now.Add(10 * time.Minute),
		},
		{
			ID: "shard-3", Group: "cloudedge", Type: ShardAssignedEventType,
			Payload:   map[string]string{"pool": "cloudedge", "prefix": "10.1.0.0/25", "node": "aws-a"},
			ExpiresAt: now.Add(10 * time.Minute),
		},
	}

	prefixes := ResolveShardScope(store, "cloudedge", "cloudedge", "oci-a", now)
	if len(prefixes) != 2 {
		t.Fatalf("expected 2 prefixes for oci-a, got %d: %v", len(prefixes), prefixes)
	}
	if prefixes[0] != "10.0.1.0/25" || prefixes[1] != "10.0.2.0/25" {
		t.Fatalf("unexpected prefixes: %v", prefixes)
	}

	prefixes = ResolveShardScope(store, "cloudedge", "cloudedge", "aws-a", now)
	if len(prefixes) != 1 || prefixes[0] != "10.1.0.0/25" {
		t.Fatalf("unexpected prefixes for aws-a: %v", prefixes)
	}

	prefixes = ResolveShardScope(store, "cloudedge", "cloudedge", "unknown-node", now)
	if len(prefixes) != 0 {
		t.Fatalf("expected no prefixes for unknown node, got %v", prefixes)
	}
}

func TestResolveShardScopeIgnoresExpired(t *testing.T) {
	store := newFakeShardStore()
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	store.events = []routerstate.EventRecord{
		{
			ID: "shard-1", Group: "cloudedge", Type: ShardAssignedEventType,
			Payload:   map[string]string{"pool": "cloudedge", "prefix": "10.0.1.0/25", "node": "oci-a"},
			ExpiresAt: now.Add(-1 * time.Minute),
		},
	}

	prefixes := ResolveShardScope(store, "cloudedge", "cloudedge", "oci-a", now)
	if len(prefixes) != 0 {
		t.Fatalf("expected no prefixes for expired event, got %v", prefixes)
	}
}

func TestResolveShardScopeIgnoresDifferentPool(t *testing.T) {
	store := newFakeShardStore()
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	store.events = []routerstate.EventRecord{
		{
			ID: "shard-1", Group: "cloudedge", Type: ShardAssignedEventType,
			Payload:   map[string]string{"pool": "other-pool", "prefix": "10.0.1.0/25", "node": "oci-a"},
			ExpiresAt: now.Add(10 * time.Minute),
		},
	}

	prefixes := ResolveShardScope(store, "cloudedge", "cloudedge", "oci-a", now)
	if len(prefixes) != 0 {
		t.Fatalf("expected no prefixes for different pool, got %v", prefixes)
	}
}
