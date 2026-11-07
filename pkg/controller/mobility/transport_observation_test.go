// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
)

func TestTransportStatusReadFailurePreservesExistingDesiredArtifacts(t *testing.T) {
	for _, mode := range []string{"remote-peer", "local-identity", "optional-direct"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
			store := testStore(t, now)
			source := api.StatusValueSourceSpec{Resource: "DHCPv4Client/endpoint", Field: "currentAddress", Optional: true}
			if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "endpoint", map[string]any{"currentAddress": "203.0.113.20"}); err != nil {
				t.Fatal(err)
			}
			router := transportRouter("test", "local", []api.SAMTransportPeerSpec{{NodeRef: "remote", RemoteEndpointFrom: source}})
			spec, err := router.Spec.Resources[0].SAMTransportProfileSpec()
			if err != nil {
				t.Fatal(err)
			}
			if mode == "local-identity" {
				router = transportRouter("test", "local", []api.SAMTransportPeerSpec{{NodeRef: "remote", RemoteEndpoint: "203.0.113.30"}})
				spec, _ = router.Spec.Resources[0].SAMTransportProfileSpec()
				spec.PublishPeerGroup = true
				spec.LocalEndpoint = ""
				spec.LocalEndpointFrom = source
			}
			if mode == "optional-direct" {
				spec.AddressingMode = "pair-stable"
				spec.Encryption = "none"
				spec.PeersFrom = []api.SAMTransportPeersSourceSpec{{Resource: "SAMRRSet/fallback"}, {Resource: "SAMPeerGroup/direct", Direct: true, Optional: true}}
				router.Spec.Resources = append(router.Spec.Resources,
					samRRSetResource("fallback", []api.SAMNodeSpec{{NodeRef: "rr", SAMEndpoint: "203.0.113.30", RouteReflector: true}}),
					samDirectPeerGroupResource("direct", "SAMEnrollmentPolicy/cloudedge-leaves", mobilityconfig.SAMTransportMeshFingerprint(spec), []api.SAMNodeSpec{{NodeRef: "remote", SAMEndpointFrom: source}}),
				)
			}
			router.Spec.Resources[0].Spec = spec
			faults := &poolSnapshotFaultStore{SQLiteStore: store, failure: errors.New("endpoint observation unavailable")}
			controller := TransportController{Router: router, Store: faults, Now: func() time.Time { return now }}
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			partSource := TransportDynamicSource("test", "local")
			before := latestPart(t, store, partSource)
			resources := decodeResources(t, before.ResourcesJSON)
			if mode == "optional-direct" && countResources(resources, api.HybridAPIVersion, "TunnelInterface") != 2 {
				t.Fatal("fixture must establish direct plus fallback before failure")
			}
			faults.failKind = "DHCPv4Client"
			now = now.Add(time.Second)
			if err := controller.Reconcile(context.Background()); !errors.Is(err, faults.failure) {
				t.Fatalf("unknown endpoint error=%v", err)
			}
			after := latestPart(t, store, partSource)
			if before.Digest != after.Digest || !before.ObservedAt.Equal(after.ObservedAt) {
				t.Fatal("failed status read replaced or renewed the desired artifacts")
			}
			if got := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "test")["phase"]; got != "Degraded" {
				t.Fatalf("failure status=%v", got)
			}
			faults.failKind = ""
			if err := controller.Reconcile(context.Background()); err != nil {
				t.Fatal(err)
			}
			if status := store.ObjectStatus(api.MobilityAPIVersion, "SAMTransportProfile", "test"); status["phase"] != "Derived" || status["reason"] != "" {
				t.Fatalf("recovery retained failure: %+v", status)
			}
		})
	}
}

func TestCanceledTransportReconcileDoesNotPublishDesiredArtifacts(t *testing.T) {
	now := time.Now()
	store := testStore(t, now)
	controller := TransportController{Router: transportRouter("test", "local", []api.SAMTransportPeerSpec{{NodeRef: "remote", RemoteEndpoint: "203.0.113.20"}}), Store: store, Now: func() time.Time { return now }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := controller.Reconcile(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reconcile=%v", err)
	}
	parts, err := store.ListDynamicConfigParts()
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 0 {
		t.Fatalf("canceled owner published desired artifacts: %+v", parts)
	}
}
