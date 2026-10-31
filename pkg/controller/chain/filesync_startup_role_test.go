// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/ha"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// Exercise the registered FileSync caller and its actual subprocess boundary.
// No peer or network service is involved: the private executable records an
// attempted copy, which must not be authorized by a previous generation's role.
func TestLeaseSyncStartupCannotUsePersistedMaster(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "attempted")
	executable := filepath.Join(dir, "copy")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n: > '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	lease := filepath.Join(dir, "leases")
	if err := os.WriteFile(lease, []byte("private lease data\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// A future timestamp deliberately rules out wall-clock freshness as proof.
	if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "gateway", map[string]any{"phase": "Applied", "role": "master", "observedAt": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "gateway"}, Spec: api.VirtualAddressSpec{Interface: "lan", Address: "192.0.2.1/32", Family: "ipv4", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 18, Priority: 200}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DHCPv4ServerSpec{LeaseFile: lease}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync"}, Metadata: api.ObjectMeta{Name: "leases"}, Spec: api.DHCPv4ServerLeaseSyncSpec{Source: api.DHCPv4ServerLeaseSyncSourceSpec{Resource: "DHCPv4Server/lan"}, Targets: []api.LeaseSyncTargetSpec{{Host: "private-unused.invalid"}}, Command: executable, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/gateway.status.role": {Equals: "master"}}}}},
	}}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wakeups := bus.New()
	runner := &Runner{Router: router, Store: store, Bus: wakeups, Opts: Options{EnabledControllers: []string{"dhcp-lease-sync"}}}
	controllers, _, _, err := runner.frameworkControllers(t.Context(), logger, eventedStore{Store: store, Bus: wakeups, Router: router}, false, ha.Decision{Leader: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(controllers) != 1 {
		t.Fatalf("controllers=%d", len(controllers))
	}
	runtime := framework.Runner{Logger: logger}
	if err := runtime.Bootstrap(context.Background(), controllers...); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("previous MASTER authorized a copy before this generation observed VRRP: stat=%v", err)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4ServerLeaseSync", "leases")
	if err != nil {
		t.Fatal(err)
	}
	if status["phase"] == "Synced" {
		t.Fatalf("unobserved role reported a successful sync: %v", status)
	}
}
