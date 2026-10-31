// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	vrrpcontroller "github.com/imksoo/routerd/pkg/controller/vrrp"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/platform"
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

func TestLeaseSyncRoleProjectionPreservesWhenAlternatives(t *testing.T) {
	dir := t.TempDir()
	lease := filepath.Join(dir, "leases")
	if err := os.WriteFile(lease, []byte("private leases"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "gateway", map[string]any{"role": "master"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "independent", map[string]any{"phase": "Ready"}); err != nil {
		t.Fatal(err)
	}
	// The status evaluator falls back to state variables when a field is absent.
	// The same persisted value there must not bypass the projected unknown role.
	if _, err := store.Set("VirtualAddress/gateway.status.role", "master", "previous process"); err != nil {
		t.Fatal(err)
	}
	role := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/gateway.status.role": {Equals: "master"}}}
	independent := api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"Interface/independent.status.phase": {Equals: "Ready"}}}
	var observed atomic.Pointer[leaseSyncRoleProof]
	cases := []struct {
		name   string
		when   api.ResourceWhenSpec
		proof  bool
		copies int
	}{
		{"persisted role", role, false, 0},
		{"all still requires role", api.ResourceWhenSpec{All: []api.ResourceWhenSpec{role, independent}}, false, 0},
		{"independent any alternative", api.ResourceWhenSpec{Any: []api.ResourceWhenSpec{role, independent}}, false, 1},
		{"unconditional", api.ResourceWhenSpec{}, false, 1},
		{"observed current role", role, true, 1},
		{"failed subsequent observation", role, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observed.Store(&leaseSyncRoleProof{names: map[string]bool{"gateway": tc.proof}})
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DHCPv4ServerSpec{LeaseFile: lease}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync"}, Metadata: api.ObjectMeta{Name: "leases"}, Spec: api.DHCPv4ServerLeaseSyncSpec{Source: api.DHCPv4ServerLeaseSyncSourceSpec{Resource: "DHCPv4Server/lan"}, Targets: []api.LeaseSyncTargetSpec{{Host: "private-unused.invalid"}}, Interval: "1ns", When: tc.when}},
			}}}
			copies := 0
			current := FileSyncController{Router: router, Store: leaseSyncRoleStore{eventedStore: eventedStore{Store: store, Router: router}, rolesObserved: &observed}, Command: func(context.Context, string, ...string) ([]byte, error) { copies++; return nil, nil }}
			if err := current.Reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			if copies != tc.copies {
				t.Fatalf("copy effects=%d want=%d", copies, tc.copies)
			}
		})
	}
}

func TestLeaseSyncBootstrapFollowsVRRPRegistration(t *testing.T) {
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{}
	runner := &Runner{Router: router, Store: store, Bus: bus.New(), Opts: Options{EnabledControllers: []string{"vrrp", "dhcp-lease-sync"}, DryRunVRRP: true}}
	controllers, _, _, err := runner.frameworkControllers(t.Context(), slog.New(slog.NewTextHandler(io.Discard, nil)), eventedStore{Store: store, Router: router}, false, ha.Decision{Leader: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(controllers) != 2 || controllers[0].Name() != "vrrp" || controllers[1].Name() != "dhcp-lease-sync" {
		t.Fatalf("startup order: %v", controllers)
	}
	registered := controllers[1].(framework.FuncController)
	if registered.EventLockKey != "dhcp-lease-sync" {
		t.Fatalf("runtime event lock changed: %q", registered.EventLockKey)
	}
}

// A periodic observation may publish role events before returning. Previously
// confirmed same-generation authority must remain available during that read;
// an actual failed/unknown observation must close it when the result is known.
func TestLeaseSyncKeepsCurrentProofUntilNativeObservationReturns(t *testing.T) {
	dir := t.TempDir()
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lease := filepath.Join(dir, "leases")
	if err := os.WriteFile(lease, []byte("private leases"), 0600); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "gateway"}, Spec: api.VirtualAddressSpec{Interface: "lan", Address: "192.0.2.1/32", Family: "ipv4", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 18, Priority: 200}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DHCPv4ServerSpec{LeaseFile: lease}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync"}, Metadata: api.ObjectMeta{Name: "leases"}, Spec: api.DHCPv4ServerLeaseSyncSpec{Source: api.DHCPv4ServerLeaseSyncSourceSpec{Resource: "DHCPv4Server/lan"}, Targets: []api.LeaseSyncTargetSpec{{Host: "private-unused.invalid"}}, Interval: "1ns", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/gateway.status.role": {Equals: "master"}}}}},
	}}}
	var proof atomic.Pointer[leaseSyncRoleProof]
	copies := 0
	consumer := FileSyncController{Router: router, Store: leaseSyncRoleStore{eventedStore: eventedStore{Store: store, Router: router}, rolesObserved: &proof}, Command: func(context.Context, string, ...string) ([]byte, error) { copies++; return nil, nil }}
	observingAgain := false
	producer := vrrpcontroller.Controller{Router: router, Store: store, OperatingSystem: platform.OSLinux, ConfigPath: filepath.Join(dir, "keepalived.conf"), Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "ip" && strings.Join(args, " ") == "-4 addr show dev ens19" {
			if observingAgain {
				if !(proof.Load() != nil && proof.Load().names["gateway"]) {
					t.Fatal("previous current-generation proof was closed before a new result existed")
				}
				if err := consumer.Reconcile(t.Context()); err != nil {
					t.Fatal(err)
				}
				return nil, errors.New("native role observation failed")
			}
			return []byte("inet 192.0.2.1/32 scope global ens19\n"), nil
		}
		return []byte("active\n"), nil
	}}
	if err := reconcileLeaseSyncVRRPRoles(t.Context(), &producer, &proof); err != nil {
		t.Fatal(err)
	}
	if !(proof.Load() != nil && proof.Load().names["gateway"]) {
		t.Fatal("actual native role success did not open the same-generation proof")
	}
	observingAgain = true
	if err := reconcileLeaseSyncVRRPRoles(t.Context(), &producer, &proof); err != nil {
		t.Fatal(err)
	}
	if copies != 1 {
		t.Fatalf("current native role could not authorize its subscribed consumer: copies=%d", copies)
	}
	if proof.Load() != nil && proof.Load().names["gateway"] {
		t.Fatal("unknown native result retained role authority")
	}
	// A stale status restored by another writer cannot reopen this in-memory proof.
	if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "gateway", map[string]any{"role": "master"}); err != nil {
		t.Fatal(err)
	}
	if err := consumer.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if copies != 1 {
		t.Fatalf("old MASTER authorized another copy after unknown observation: copies=%d", copies)
	}
}

// A's current native proof must not depend on an unrelated B excluded by When.
// B's prior MASTER and its persisted variable fallback still grant no authority.
func TestLeaseSyncOnlyRequiresReferencedNativeRole(t *testing.T) {
	dir := t.TempDir()
	store, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lease := filepath.Join(dir, "leases")
	if err := os.WriteFile(lease, []byte("private leases"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "blocked", map[string]any{"phase": "Down"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", "excluded", map[string]any{"role": "master"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Set("VirtualAddress/excluded.status.role", "master", "previous generation"); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "gateway"}, Spec: api.VirtualAddressSpec{Interface: "lan", Address: "192.0.2.1/32", Family: "ipv4", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 18, Priority: 200}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "excluded"}, Spec: api.VirtualAddressSpec{Interface: "lan", Address: "192.0.2.2/32", Family: "ipv4", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 19, Priority: 200}, When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"Interface/blocked.status.phase": {Equals: "Ready"}}}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Server"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.DHCPv4ServerSpec{LeaseFile: lease}},
	}}}
	for _, name := range []string{"gateway", "excluded"} {
		router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync"}, Metadata: api.ObjectMeta{Name: "leases-" + name}, Spec: api.DHCPv4ServerLeaseSyncSpec{
			Source: api.DHCPv4ServerLeaseSyncSourceSpec{Resource: "DHCPv4Server/lan"}, Targets: []api.LeaseSyncTargetSpec{{Host: "private-unused.invalid"}}, Interval: "1ns",
			When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/" + name + ".status.role": {Equals: "master"}}},
		}})
	}
	evented := eventedStore{Store: store, Router: router}
	runner := Runner{Router: router, Store: store}
	initial, err := runner.effectiveRouterForReconcile(evented)
	if err != nil {
		t.Fatal(err)
	}
	var proof atomic.Pointer[leaseSyncRoleProof]
	producer := vrrpcontroller.Controller{Router: initial.EffectiveRouter, Store: store, OperatingSystem: platform.OSLinux, ConfigPath: filepath.Join(dir, "keepalived.conf"), Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "ip" && strings.Join(args, " ") == "-4 addr show dev ens19" {
			return []byte("inet 192.0.2.1/32 scope global ens19\n"), nil
		}
		return []byte("active\n"), nil
	}}
	if err := reconcileLeaseSyncVRRPRoles(t.Context(), &producer, &proof); err != nil {
		t.Fatal(err)
	}
	observed := proof.Load()
	if observed == nil || !observed.names["gateway"] || observed.names["excluded"] {
		t.Fatalf("native proof=%v", observed)
	}
	// Use the actual caller's post-observation view as well. The excluded B has
	// no native proof even though the persisted state-variable fallback is MASTER.
	current, err := runner.effectiveRouterForReconcile(evented)
	if err != nil {
		t.Fatal(err)
	}
	copies := 0
	consumer := FileSyncController{Router: current.EffectiveRouter, Store: leaseSyncRoleStore{eventedStore: evented, rolesObserved: &proof}, Command: func(context.Context, string, ...string) ([]byte, error) { copies++; return nil, nil }}
	if err := consumer.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if copies != 1 {
		t.Fatalf("observed A must sync, excluded B must not: copies=%d", copies)
	}
	for name, phase := range map[string]string{"gateway": "Synced", "excluded": "Pending"} {
		status, err := store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4ServerLeaseSync", "leases-"+name)
		if err != nil || status["phase"] != phase {
			t.Fatalf("%s status=%v error=%v; want %s", name, status, err, phase)
		}
	}
}
