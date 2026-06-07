// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestStatusWithOwnershipAddsControllerMetadata(t *testing.T) {
	status := statusWithOwnership("net.routerd.net/v1alpha1", "EgressRoutePolicy", map[string]any{"phase": "Applied"})
	if status["owner"] != "route" {
		t.Fatalf("owner = %v, want route", status["owner"])
	}
	if status["managedBy"] != "routerd" || status["management"] != "managed" {
		t.Fatalf("management metadata = managedBy:%v management:%v, want routerd/managed", status["managedBy"], status["management"])
	}
}

func TestStatusWithOwnershipPreservesAdoptedManagedBy(t *testing.T) {
	status := statusWithOwnership("system.routerd.net/v1alpha1", "NetworkAdoption", map[string]any{
		"phase":     "Observed",
		"managed":   false,
		"managedBy": "systemd-networkd",
	})
	if status["owner"] != "network-adoption" {
		t.Fatalf("owner = %v, want network-adoption", status["owner"])
	}
	if status["managedBy"] != "systemd-networkd" || status["management"] != "adopted" {
		t.Fatalf("management metadata = managedBy:%v management:%v, want systemd-networkd/adopted", status["managedBy"], status["management"])
	}
}

func TestEventedStoreAddsLifecycleOwnerMetadata(t *testing.T) {
	base := mapStore{}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"},
		Metadata: api.ObjectMeta{Name: "fabric"},
		Spec:     api.BGPPeerSpec{RouterRef: "core", PeerASN: 64512, Peers: []string{"192.0.2.1"}},
	}}}}
	store := eventedStore{Store: base, Router: router}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "BGPPeer", "fabric", map[string]any{"phase": "Established"}); err != nil {
		t.Fatalf("save status: %v", err)
	}
	status := base.ObjectStatus(api.NetAPIVersion, "BGPPeer", "fabric")
	if status["ownerKey"] != api.NetAPIVersion+"/BGPPeer/fabric" {
		t.Fatalf("ownerKey = %v", status["ownerKey"])
	}
	ownerRef, ok := status["ownerRef"].(map[string]any)
	if !ok || ownerRef["apiVersion"] != api.NetAPIVersion || ownerRef["kind"] != "BGPPeer" || ownerRef["name"] != "fabric" {
		t.Fatalf("ownerRef = %#v", status["ownerRef"])
	}
	if status["lifecycleClass"] != "controller" {
		t.Fatalf("lifecycleClass = %v, want controller", status["lifecycleClass"])
	}
}

func TestEventedStoreAddsDerivedOwnerRefs(t *testing.T) {
	base := mapStore{}
	root := api.OwnerRef{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile", Name: "fabric"}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "sam-core-a", OwnerRefs: []api.OwnerRef{root}},
		Spec:     api.TunnelInterfaceSpec{Mode: "ipip", Local: "10.99.0.1", Remote: "10.99.0.2"},
	}}}}
	store := eventedStore{Store: base, Router: router}
	if err := store.SaveObjectStatus(api.HybridAPIVersion, "TunnelInterface", "sam-core-a", map[string]any{"phase": "Applied"}); err != nil {
		t.Fatalf("save status: %v", err)
	}
	status := base.ObjectStatus(api.HybridAPIVersion, "TunnelInterface", "sam-core-a")
	if status["ownerKey"] != api.HybridAPIVersion+"/TunnelInterface/sam-core-a" {
		t.Fatalf("ownerKey = %v", status["ownerKey"])
	}
	refs, ok := status["ownerRefs"].([]any)
	if !ok || len(refs) != 1 {
		t.Fatalf("ownerRefs = %#v", status["ownerRefs"])
	}
	ref, ok := refs[0].(map[string]any)
	if !ok || ref["apiVersion"] != api.MobilityAPIVersion || ref["kind"] != "SAMTransportProfile" || ref["name"] != "fabric" {
		t.Fatalf("ownerRefs[0] = %#v", refs[0])
	}
	if status["lifecycleClass"] != "managed-host" {
		t.Fatalf("lifecycleClass = %v, want managed-host", status["lifecycleClass"])
	}
}

func TestEventedStoreSkipsLifecycleMetadataForSyntheticStatus(t *testing.T) {
	base := mapStore{}
	store := eventedStore{Store: base}
	if err := store.SaveObjectStatus(api.RouterAPIVersion, "Inventory", "host", map[string]any{"phase": "Observed"}); err != nil {
		t.Fatalf("save status: %v", err)
	}
	status := base.ObjectStatus(api.RouterAPIVersion, "Inventory", "host")
	for _, key := range []string{"ownerKey", "ownerRef", "ownerRefs", "lifecycleClass"} {
		if _, ok := status[key]; ok {
			t.Fatalf("synthetic status has %s: %#v", key, status)
		}
	}
}

func TestStatusChangedIgnoresObservedTrafficCounters(t *testing.T) {
	current := map[string]any{
		"phase":       "Observed",
		"path":        "/var/lib/routerd/traffic-flows.db",
		"source":      "conntrack",
		"activeFlows": 10,
		"count":       100,
		"observedAt":  "2026-05-05T00:00:00Z",
	}
	next := map[string]any{
		"phase":       "Observed",
		"path":        "/var/lib/routerd/traffic-flows.db",
		"source":      "conntrack",
		"activeFlows": 20,
		"count":       200,
		"observedAt":  "2026-05-05T00:00:30Z",
	}
	if statusChanged(current, next) {
		t.Fatalf("Observed counter-only update should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestStatusChangedIgnoresPathMTUObservationTimestamp(t *testing.T) {
	current := map[string]any{
		"phase":         "Applied",
		"mtu":           float64(1445),
		"mtuSource":     "probe",
		"mtuObservedAt": "2026-05-12T00:52:18Z",
	}
	next := map[string]any{
		"phase":         "Applied",
		"mtu":           1445,
		"mtuSource":     "probe",
		"mtuObservedAt": "2026-05-12T01:02:43Z",
	}
	if statusChanged(current, next) {
		t.Fatalf("path MTU observation timestamp-only update should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}

	next["mtu"] = 1444
	fields := statusChangedFields(current, next)
	if len(fields) != 1 || fields[0] != "mtu" {
		t.Fatalf("changed fields = %v, want [mtu]", fields)
	}
}

func TestStatusChangedIgnoresLastTransitionAt(t *testing.T) {
	current := map[string]any{
		"phase":             "Applied",
		"selectedCandidate": "ds-lite-ra",
		"selectedDevice":    "ds-lite-ra",
		"lastTransitionAt":  "2026-05-20T10:00:00Z",
	}
	next := map[string]any{
		"phase":             "Applied",
		"selectedCandidate": "ds-lite-ra",
		"selectedDevice":    "ds-lite-ra",
		"lastTransitionAt":  "2026-05-20T10:00:30Z",
	}
	if statusChanged(current, next) {
		t.Fatalf("lastTransitionAt-only update should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestEventedStoreDoesNotPublishTimestampOnlyStatusChange(t *testing.T) {
	base := mapStore{
		api.NetAPIVersion + "/EgressRoutePolicy/ipv4-default": statusWithOwnership(api.NetAPIVersion, "EgressRoutePolicy", map[string]any{
			"phase":             "Applied",
			"selectedCandidate": "ds-lite-ra",
			"selectedDevice":    "ds-lite-ra",
			"lastTransitionAt":  "2026-05-20T10:00:00Z",
		}),
	}
	eventBus := bus.New()
	resource := daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy", Name: "ipv4-default"}
	ch, cancel := eventBus.Subscribe(context.Background(), bus.Subscription{
		Topics:   []string{"routerd.resource.status.changed"},
		Resource: &resource,
	}, 1)
	defer cancel()

	store := eventedStore{Store: base, Bus: eventBus}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default", map[string]any{
		"phase":             "Applied",
		"selectedCandidate": "ds-lite-ra",
		"selectedDevice":    "ds-lite-ra",
		"lastTransitionAt":  "2026-05-20T10:00:30Z",
	}); err != nil {
		t.Fatalf("save status: %v", err)
	}

	select {
	case event := <-ch:
		t.Fatalf("unexpected event: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}

	if err := store.SaveObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default", map[string]any{
		"phase":             "Applied",
		"selectedCandidate": "ix2215",
		"selectedDevice":    "ix2215",
		"lastTransitionAt":  "2026-05-20T10:01:00Z",
	}); err != nil {
		t.Fatalf("save changed status: %v", err)
	}

	select {
	case event := <-ch:
		if fields := event.Attributes["changedFields"]; !strings.Contains(fields, "selectedCandidate") || strings.Contains(fields, "lastTransitionAt") {
			t.Fatalf("changedFields = %q, want selectedCandidate without lastTransitionAt", fields)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for semantic status change event")
	}
}

func TestEventedStoreDoesNotPublishMobilityTimestampOnlyStatusRefresh(t *testing.T) {
	base := mapStore{
		api.MobilityAPIVersion + "/MobilityPool/cloudedge": statusWithOwnership(api.MobilityAPIVersion, "MobilityPool", map[string]any{
			"plannerPhase":       "Planned",
			"phase":              "Projected",
			"dynamicDigest":      "sha256:abc",
			"generatedClaims":    1,
			"generatedActions":   2,
			"placementActive":    false,
			"plannedAt":          "2026-06-01T10:00:00Z",
			"projectedAt":        "2026-06-01T10:00:00Z",
			"dynamicExpiresAt":   "2026-06-01T10:05:00Z",
			"operatorIntent":     "MobilityPool",
			"derivedConfigKinds": []string{"AddressMobilityDomain", "RemoteAddressClaim"},
		}),
	}
	eventBus := bus.New()
	resource := daemonapi.ResourceRef{APIVersion: api.MobilityAPIVersion, Kind: "MobilityPool", Name: "cloudedge"}
	ch, cancel := eventBus.Subscribe(context.Background(), bus.Subscription{
		Topics:   []string{"routerd.resource.status.changed"},
		Resource: &resource,
	}, 1)
	defer cancel()

	store := eventedStore{Store: base, Bus: eventBus}
	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase":       "Planned",
		"phase":              "Projected",
		"dynamicDigest":      "sha256:abc",
		"generatedClaims":    1,
		"generatedActions":   2,
		"placementActive":    false,
		"plannedAt":          "2026-06-01T10:00:30Z",
		"projectedAt":        "2026-06-01T10:00:30Z",
		"dynamicExpiresAt":   "2026-06-01T10:05:30Z",
		"operatorIntent":     "MobilityPool",
		"derivedConfigKinds": []string{"AddressMobilityDomain", "RemoteAddressClaim"},
	}); err != nil {
		t.Fatalf("save mobility status refresh: %v", err)
	}

	select {
	case event := <-ch:
		t.Fatalf("unexpected mobility timestamp-only event: %#v", event)
	case <-time.After(20 * time.Millisecond):
	}
	if got := base.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge")["plannedAt"]; got != "2026-06-01T10:00:30Z" {
		t.Fatalf("plannedAt was not persisted: %v", got)
	}

	if err := store.SaveObjectStatus(api.MobilityAPIVersion, "MobilityPool", "cloudedge", map[string]any{
		"plannerPhase":       "Planned",
		"phase":              "Projected",
		"dynamicDigest":      "sha256:def",
		"generatedClaims":    1,
		"generatedActions":   3,
		"placementActive":    false,
		"plannedAt":          "2026-06-01T10:01:00Z",
		"projectedAt":        "2026-06-01T10:01:00Z",
		"dynamicExpiresAt":   "2026-06-01T10:06:00Z",
		"operatorIntent":     "MobilityPool",
		"derivedConfigKinds": []string{"AddressMobilityDomain", "RemoteAddressClaim"},
	}); err != nil {
		t.Fatalf("save semantic mobility status: %v", err)
	}

	select {
	case event := <-ch:
		fields := event.Attributes["changedFields"]
		if !strings.Contains(fields, "dynamicDigest") || !strings.Contains(fields, "generatedActions") {
			t.Fatalf("changedFields = %q, want semantic fields", fields)
		}
		for _, volatile := range []string{"plannedAt", "projectedAt", "dynamicExpiresAt"} {
			if strings.Contains(fields, volatile) {
				t.Fatalf("changedFields = %q, should omit volatile %s", fields, volatile)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for semantic mobility status event")
	}
}

func TestMobilityStatusEventComparisonKeepsBehavioralFields(t *testing.T) {
	current := map[string]any{
		"phase":               "Projected",
		"plannerPhase":        "Planned",
		"owner":               "mobility",
		"holder":              "azure-router-a",
		"captureStatus":       "Captured",
		"captureActive":       true,
		"allowReassignment":   false,
		"deliveryRoute":       "Installed",
		"generatedActions":    1,
		"plannedAt":           "2026-06-01T10:00:00Z",
		"projectedAt":         "2026-06-01T10:00:00Z",
		"dynamicExpiresAt":    "2026-06-01T10:05:00Z",
		"streamMaxObservedAt": "2026-06-01T10:00:00Z",
	}
	for _, field := range []string{
		"phase",
		"plannerPhase",
		"owner",
		"holder",
		"captureStatus",
		"captureActive",
		"allowReassignment",
		"deliveryRoute",
		"generatedActions",
		"streamMaxObservedAt",
	} {
		next := map[string]any{}
		for key, value := range current {
			next[key] = value
		}
		next[field] = changedMobilityStatusValue(current[field])
		if !statusChangedForEvent(api.MobilityAPIVersion, "MobilityPool", current, next) {
			t.Fatalf("mobility status field %s must remain event-significant", field)
		}
	}

	next := map[string]any{}
	for key, value := range current {
		next[key] = value
	}
	next["plannedAt"] = "2026-06-01T10:00:30Z"
	next["projectedAt"] = "2026-06-01T10:00:30Z"
	next["dynamicExpiresAt"] = "2026-06-01T10:05:30Z"
	if statusChangedForEvent(api.MobilityAPIVersion, "MobilityPool", current, next) {
		t.Fatalf("mobility timestamp-only refresh should not be event-significant")
	}
}

func changedMobilityStatusValue(value any) any {
	switch typed := value.(type) {
	case bool:
		return !typed
	case int:
		return typed + 1
	case string:
		return typed + "-changed"
	default:
		return "changed"
	}
}

func TestStatusChangedIgnoresRuntimeTelemetry(t *testing.T) {
	current := map[string]any{
		"phase":               "Connected",
		"publicKey":           "peer-key",
		"allowedIPs":          []any{"10.99.0.2/32"},
		"latestHandshake":     "2026-05-13T06:30:00Z",
		"handshakeAgeSeconds": float64(12),
		"transferRxBytes":     float64(1000),
		"transferTxBytes":     float64(2000),
	}
	next := map[string]any{
		"phase":               "Connected",
		"publicKey":           "peer-key",
		"allowedIPs":          []string{"10.99.0.2/32"},
		"latestHandshake":     "2026-05-13T06:31:00Z",
		"handshakeAgeSeconds": 1,
		"transferRxBytes":     uint64(3000),
		"transferTxBytes":     uint64(4000),
	}
	if statusChanged(current, next) {
		t.Fatalf("runtime telemetry-only update should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestStatusChangedTreatsNilStringSlicesAsNull(t *testing.T) {
	current := map[string]any{
		"phase":            "Active",
		"destinationCIDRs": nil,
		"skipped":          nil,
	}
	next := map[string]any{
		"phase":            "Active",
		"destinationCIDRs": []string(nil),
		"skipped":          []string(nil),
	}
	if statusChanged(current, next) {
		t.Fatalf("nil string slice should be stable against stored null")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestStatusChangedIgnoresRenderDiagnostics(t *testing.T) {
	current := map[string]any{
		"phase":         "Applied",
		"backend":       "nftables",
		"internalHoles": float64(10),
	}
	next := map[string]any{
		"phase":         "Applied",
		"backend":       "nftables",
		"internalHoles": 11,
	}
	if statusChanged(current, next) {
		t.Fatalf("render diagnostic-only update should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestStatusChangedIgnoresNestedTrackCounters(t *testing.T) {
	current := map[string]any{
		"phase": "Applied",
		"track": []any{map[string]any{
			"resource":       "BGPRouter/lab",
			"healthy":        true,
			"healthyCount":   float64(10),
			"unhealthyCount": float64(0),
		}},
	}
	next := map[string]any{
		"phase": "Applied",
		"track": []map[string]any{{
			"resource":       "BGPRouter/lab",
			"healthy":        true,
			"healthyCount":   11,
			"unhealthyCount": 0,
		}},
	}
	if statusChanged(current, next) {
		t.Fatalf("nested track counter-only update should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestStatusChangedNormalizesStructSlices(t *testing.T) {
	type backend struct {
		Name            string `json:"name"`
		ResolvedAddress string `json:"resolvedAddress"`
		Port            int    `json:"port"`
		Healthy         bool   `json:"healthy"`
	}
	current := map[string]any{
		"phase": "Active",
		"backends": []any{map[string]any{
			"name":            "router06-ssh",
			"resolvedAddress": "192.168.123.111",
			"port":            float64(22),
			"healthy":         true,
		}},
	}
	next := map[string]any{
		"phase": "Active",
		"backends": []backend{{
			Name:            "router06-ssh",
			ResolvedAddress: "192.168.123.111",
			Port:            22,
			Healthy:         true,
		}},
	}
	if statusChanged(current, next) {
		t.Fatalf("struct slice equivalent to stored map slice should not be a resource status change")
	}
	if fields := statusChangedFields(current, next); len(fields) != 0 {
		t.Fatalf("changed fields = %v, want none", fields)
	}
}

func TestStatusChangedIgnoresPeerDetailTelemetry(t *testing.T) {
	current := map[string]any{
		"phase":           "Running",
		"backendState":    "Running",
		"onlinePeerCount": 2,
		"peers": []map[string]any{{
			"id":       "peer-a",
			"online":   true,
			"lastSeen": "2026-05-13T06:30:00Z",
			"rxBytes":  100,
			"txBytes":  200,
		}},
	}
	next := map[string]any{
		"phase":           "Running",
		"backendState":    "Running",
		"onlinePeerCount": 2,
		"peers": []map[string]any{{
			"id":       "peer-a",
			"online":   true,
			"lastSeen": "2026-05-13T06:31:00Z",
			"rxBytes":  300,
			"txBytes":  400,
		}},
	}
	if statusChanged(current, next) {
		t.Fatalf("peer detail telemetry-only update should not be a resource status change")
	}
	next["onlinePeerCount"] = 1
	fields := statusChangedFields(current, next)
	if len(fields) != 1 || fields[0] != "onlinePeerCount" {
		t.Fatalf("changed fields = %v, want [onlinePeerCount]", fields)
	}
}

func TestStatusChangedFieldsReportsMeaningfulChanges(t *testing.T) {
	current := map[string]any{
		"phase":             "Applied",
		"selectedDevice":    "ds-lite",
		"previousNoise":     "same",
		"updatedAt":         "2026-05-05T00:00:00Z",
		"consecutivePassed": 1,
	}
	next := map[string]any{
		"phase":             "Applied",
		"selectedDevice":    "ix2215",
		"previousNoise":     "same",
		"updatedAt":         "2026-05-05T00:00:30Z",
		"consecutivePassed": 2,
	}
	fields := statusChangedFields(current, next)
	if len(fields) != 1 || fields[0] != "selectedDevice" {
		t.Fatalf("changed fields = %v, want [selectedDevice]", fields)
	}
}
