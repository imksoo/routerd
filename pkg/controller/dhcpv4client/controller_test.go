// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

type mapStore map[string]map[string]any

func (s mapStore) SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error {
	s[apiVersion+"/"+kind+"/"+name] = status
	return nil
}

func (s mapStore) ObjectStatus(apiVersion, kind, name string) map[string]any {
	return s[apiVersion+"/"+kind+"/"+name]
}

func TestControllerDryRunMissingDaemonSocketIsPending(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "missing.sock")
	store := mapStore{}
	controller := Controller{
		Router:        &api.Router{},
		Store:         store,
		DaemonSockets: map[string]string{"wan": socket},
		DryRun:        true,
	}
	if err := controller.Reconcile(context.Background(), "wan"); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	if status["phase"] != daemonapi.ResourcePhasePending || status["reason"] != "DaemonUnavailable" {
		t.Fatalf("status = %#v", status)
	}
	if status["socket"] != socket || status["dryRun"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func TestLeaseEventChangedIgnoresNonCoreFields(t *testing.T) {
	current := map[string]any{
		"phase":          daemonapi.ResourcePhaseBound,
		"currentAddress": "192.0.2.10",
		"prefixLength":   float64(24),
		"defaultGateway": "192.0.2.1",
		"domain":         "example.test",
		"leaseTime":      "3600",
		"appliedAddress": "192.0.2.10/24",
		"dnsServers":     []any{"192.0.2.53"},
		"ntpServers":     []any{"192.0.2.123"},
		"lastLeaseAt":    "2026-06-19T10:00:00Z",
	}
	next := map[string]any{
		"phase":          daemonapi.ResourcePhaseBound,
		"currentAddress": "192.0.2.10",
		"prefixLength":   int(24),
		"defaultGateway": "192.0.2.1",
		"domain":         "",
		"leaseTime":      "3570",
		"appliedAddress": "192.0.2.10/24",
		"dnsServers":     []string{"192.0.2.53", "8.8.8.8"},
		"ntpServers":     nil,
		"lastLeaseAt":    "2026-06-19T10:00:30Z",
	}
	if leaseEventChanged(current, next) {
		t.Fatal("non-core field changes must not emit DHCPv4 client applied event")
	}

	next["defaultGateway"] = "192.0.2.254"
	if !leaseEventChanged(current, next) {
		t.Fatal("default gateway change must emit DHCPv4 client applied event")
	}
	next["defaultGateway"] = "192.0.2.1"

	next["currentAddress"] = "192.0.2.11"
	if !leaseEventChanged(current, next) {
		t.Fatal("address change must emit DHCPv4 client applied event")
	}
	next["currentAddress"] = "192.0.2.10"

	next["phase"] = "Expired"
	if !leaseEventChanged(current, next) {
		t.Fatal("phase change must emit DHCPv4 client applied event")
	}
}

func TestLeaseEventChangedNilVsEmptyString(t *testing.T) {
	current := map[string]any{
		"phase":          nil,
		"currentAddress": nil,
		"prefixLength":   nil,
		"defaultGateway": nil,
	}
	next := map[string]any{
		"phase":          "",
		"currentAddress": "",
		"prefixLength":   "",
		"defaultGateway": "",
	}
	if leaseEventChanged(current, next) {
		t.Fatal("nil-to-empty must not be treated as a change")
	}
}
