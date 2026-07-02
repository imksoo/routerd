// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/healthcheck"
)

func TestSaveWhenFalseStatusesPreservesFreshHealthCheckDaemonStatus(t *testing.T) {
	resource := whenFalseHealthCheck("internet")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}
	checkedAt := time.Now().UTC().Add(-10 * time.Second)
	store := mapStore{
		api.NetAPIVersion + "/HealthCheck/internet": {
			"phase":             healthcheck.PhaseHealthy,
			"lastCheckedAt":     checkedAt.Format(time.RFC3339Nano),
			"lastResult":        "passed",
			"consecutiveFailed": 0,
			"consecutivePassed": 12,
		},
	}

	if err := (&Runner{Router: router}).saveWhenFalseStatuses(eventedStore{Store: store}); err != nil {
		t.Fatalf("saveWhenFalseStatuses returned error: %v", err)
	}

	status := store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "internet")
	if got := status["phase"]; got != healthcheck.PhaseHealthy {
		t.Fatalf("phase = %v, want %s", got, healthcheck.PhaseHealthy)
	}
	if got := status["reason"]; got == "WhenFalse" {
		t.Fatalf("reason = %v, want daemon status preserved", got)
	}
}

func TestSaveWhenFalseStatusesPreservesFreshHealthCheckDaemonEvidenceAfterOldWhenFalse(t *testing.T) {
	resource := whenFalseHealthCheck("internet")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}
	checkedAt := time.Now().UTC().Add(-10 * time.Second)
	observed := map[string]any{
		"phase":             healthcheck.PhaseHealthy,
		"lastCheckedAt":     checkedAt.Format(time.RFC3339Nano),
		"lastResult":        "passed",
		"consecutiveFailed": 0,
		"consecutivePassed": 12,
	}
	store := mapStore{
		api.NetAPIVersion + "/HealthCheck/internet": {
			"phase":    "Pending",
			"reason":   "WhenFalse",
			"observed": observed,
		},
	}

	if err := (&Runner{Router: router}).saveWhenFalseStatuses(eventedStore{Store: store}); err != nil {
		t.Fatalf("saveWhenFalseStatuses returned error: %v", err)
	}

	status := store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "internet")
	if got := status["phase"]; got != healthcheck.PhaseHealthy {
		t.Fatalf("phase = %v, want %s", got, healthcheck.PhaseHealthy)
	}
	if got := status["lastResult"]; got != "passed" {
		t.Fatalf("lastResult = %v, want passed", got)
	}
	if got := status["reason"]; got == "WhenFalse" {
		t.Fatalf("reason = %v, want observed daemon status promoted", got)
	}
}

func TestSaveWhenFalseStatusesMarksStaleHealthCheckWhenFalse(t *testing.T) {
	resource := whenFalseHealthCheck("internet")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}
	checkedAt := time.Now().UTC().Add(-10 * time.Minute)
	store := mapStore{
		api.NetAPIVersion + "/HealthCheck/internet": {
			"phase":         healthcheck.PhaseHealthy,
			"lastCheckedAt": checkedAt.Format(time.RFC3339Nano),
			"lastResult":    "passed",
		},
	}

	if err := (&Runner{Router: router}).saveWhenFalseStatuses(eventedStore{Store: store}); err != nil {
		t.Fatalf("saveWhenFalseStatuses returned error: %v", err)
	}

	status := store.ObjectStatus(api.NetAPIVersion, "HealthCheck", "internet")
	if got := status["phase"]; got != "Pending" {
		t.Fatalf("phase = %v, want Pending", got)
	}
	if got := status["reason"]; got != "WhenFalse" {
		t.Fatalf("reason = %v, want WhenFalse", got)
	}
}

func TestSaveWhenFalseStatusesStillMarksNonDaemonResourceWhenFalse(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "EgressRoutePolicy"},
			Metadata: api.ObjectMeta{Name: "ipv4-default"},
			Spec: api.EgressRoutePolicySpec{
				When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
					"VirtualAddress/wan.role": {Equals: "master"},
				}},
			},
		},
	}}}
	store := mapStore{}

	if err := (&Runner{Router: router}).saveWhenFalseStatuses(eventedStore{Store: store}); err != nil {
		t.Fatalf("saveWhenFalseStatuses returned error: %v", err)
	}

	status := store.ObjectStatus(api.NetAPIVersion, "EgressRoutePolicy", "ipv4-default")
	if got := status["phase"]; got != "Pending" {
		t.Fatalf("phase = %v, want Pending", got)
	}
	if got := status["reason"]; got != "WhenFalse" {
		t.Fatalf("reason = %v, want WhenFalse", got)
	}
}

func TestSaveWhenFalseStatusesPreservesStatusWhenDependencyUnknown(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
			Metadata: api.ObjectMeta{Name: "ds-lite-a-source"},
			Spec: api.IPv4StaticAddressSpec{
				Interface: "ds-lite-a",
				Address:   "192.0.0.2/29",
				When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
					"VirtualAddress/lan-gw-v4.role": {Equals: "master"},
				}},
			},
		},
	}}}
	store := mapStore{
		api.NetAPIVersion + "/VirtualAddress/lan-gw-v4": {
			"role": "unknown",
		},
		api.NetAPIVersion + "/IPv4StaticAddress/ds-lite-a-source": {
			"phase":     "Applied",
			"interface": "ds-lite-a",
			"ifname":    "ds-lite-a",
			"address":   "192.0.0.2/29",
		},
	}

	if err := (&Runner{Router: router}).saveWhenFalseStatuses(eventedStore{Store: store}); err != nil {
		t.Fatalf("saveWhenFalseStatuses returned error: %v", err)
	}

	status := store.ObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "ds-lite-a-source")
	if got := status["phase"]; got != "Applied" {
		t.Fatalf("phase = %v, want Applied", got)
	}
	if got := status["reason"]; got == "WhenFalse" {
		t.Fatalf("reason = %v, want existing status preserved while dependency is unknown", got)
	}
}

func TestDaemonObservedOnlyStatusPromotesHealthCheckObservedPhase(t *testing.T) {
	current := map[string]any{
		"phase":  "Pending",
		"reason": "WhenFalse",
	}
	base := map[string]any{
		"phase":     healthcheck.PhaseHealthy,
		"health":    "ok",
		"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	}

	status := daemonObservedOnlyStatus(current, base, daemonapi.ResourceStatus{
		Resource: daemonapi.ResourceRef{
			APIVersion: api.NetAPIVersion,
			Kind:       "HealthCheck",
			Name:       "internet",
		},
		Phase:  healthcheck.PhaseHealthy,
		Health: "ok",
		Observed: map[string]string{
			"lastCheckedAt": time.Now().UTC().Format(time.RFC3339Nano),
			"lastResult":    "passed",
		},
	})

	if got := status["phase"]; got != healthcheck.PhaseHealthy {
		t.Fatalf("phase = %v, want %s", got, healthcheck.PhaseHealthy)
	}
	if got := status["reason"]; got == "WhenFalse" {
		t.Fatalf("reason = %v, want cleared after healthcheck observation", got)
	}
	if observed := statusMap(status["observed"]); observed["phase"] != healthcheck.PhaseHealthy {
		t.Fatalf("observed phase = %v, want %s", observed["phase"], healthcheck.PhaseHealthy)
	}
}

func whenFalseHealthCheck(name string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "HealthCheck"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.HealthCheckSpec{
			Interval: "30s",
			Timeout:  "3s",
			When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{
				"VirtualAddress/lan.role": {Equals: "master"},
			}},
		},
	}
}
