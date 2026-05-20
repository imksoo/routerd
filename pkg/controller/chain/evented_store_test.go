// SPDX-License-Identifier: BSD-3-Clause

package chain

import "testing"

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
