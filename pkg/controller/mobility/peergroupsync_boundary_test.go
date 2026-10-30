// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestPeerGroupSyncDoesNotRenewExpiredRemoteLease(t *testing.T) {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	store := testStore(t, now)
	resource := samPeerGroupResource("group", []api.SAMTransportPeerSpec{{NodeRef: "rr", RemoteEndpoint: "10.0.0.1"}})
	stampSourceGeneration(&resource, routerstate.DynamicConfigPartRecord{Generation: 1, ExpiresAt: now.Add(time.Second)})
	srv := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(PeerGroupSyncResponse{PeerGroups: []api.Resource{resource}})
	}))
	defer srv.Close()
	addr, port := serverAddr(t, srv)
	reads := 0
	client := &PeerGroupSyncClient{Store: store, HTTPClient: srv.Client(), Port: port, Now: func() time.Time {
		reads++
		if reads == 1 {
			return now
		}
		return now.Add(time.Second)
	}, Discover: func(context.Context, *api.Router, string) ([]netip.Addr, error) { return []netip.Addr{addr}, nil }}
	if _, found, err := client.SyncPeerGroup(context.Background(), nil, "wg", "group"); err == nil || found {
		t.Fatalf("remote lease was revived: found=%v err=%v", found, err)
	}
	records, err := store.GetDynamicConfigPartsBySource(PeerGroupSyncDynamicSource("group"))
	if err != nil || len(records) != 0 {
		t.Fatalf("expired resource persisted: %+v err=%v", records, err)
	}
}

func TestPeerGroupSyncRejectsMissingOrMalformedLeaseMetadata(t *testing.T) {
	for _, annotations := range []map[string]string{
		nil,
		{"routerd.net/source-generation": "bad", "routerd.net/source-valid-until": time.Now().Add(time.Hour).Format(time.RFC3339Nano)},
		{"routerd.net/source-generation": "1", "routerd.net/source-valid-until": "bad"},
	} {
		t.Run(annotations["routerd.net/source-generation"]+annotations["routerd.net/source-valid-until"], func(t *testing.T) {
			now := time.Now()
			store := testStore(t, now)
			resource := samPeerGroupResource("group", []api.SAMTransportPeerSpec{{NodeRef: "rr"}})
			resource.Metadata.Annotations = annotations
			srv := newIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(PeerGroupSyncResponse{PeerGroups: []api.Resource{resource}})
			}))
			defer srv.Close()
			addr, port := serverAddr(t, srv)
			client := &PeerGroupSyncClient{Store: store, HTTPClient: srv.Client(), Port: port, Discover: func(context.Context, *api.Router, string) ([]netip.Addr, error) { return []netip.Addr{addr}, nil }}
			if _, found, err := client.SyncPeerGroup(context.Background(), nil, "wg", "group"); err == nil || found {
				t.Fatalf("unknown remote validity accepted: found=%v err=%v", found, err)
			}
		})
	}
}

func TestSyncDigestExcludesLeaseMetadataWithoutMutatingResource(t *testing.T) {
	now := time.Now()
	resource := samPeerGroupResource("group", []api.SAMTransportPeerSpec{{NodeRef: "rr"}})
	stampSourceGeneration(&resource, routerstate.DynamicConfigPartRecord{Generation: 1, ExpiresAt: now.Add(time.Minute)})
	first := resourceDigest(resource)
	if resource.Metadata.Annotations["routerd.net/source-generation"] != "1" {
		t.Fatal("hash mutated source metadata")
	}
	stampSourceGeneration(&resource, routerstate.DynamicConfigPartRecord{Generation: 2, ExpiresAt: now.Add(time.Hour)})
	if second := resourceDigest(resource); first != second {
		t.Fatalf("same desired resource conflicted only because lease refreshed: %s %s", first, second)
	}
}
