// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	bgpcontroller "github.com/imksoo/routerd/pkg/controller/bgp"
	"github.com/imksoo/routerd/pkg/manageddaemon"
	"github.com/imksoo/routerd/pkg/render"
)

type bgpStatusDeleteStore struct {
	mapStore
	fail bool
}

func (s *bgpStatusDeleteStore) DeleteObject(version, kind, name string) error {
	if s.fail && version == api.NetAPIVersion && (kind == "BGPRouter" || kind == "BGPPeer") {
		return errors.New("status deletion unavailable")
	}
	return s.mapStore.DeleteObject(version, kind, name)
}

func TestEmptyRouterRetiresBGPStatusesAfterAcknowledgedWithdrawal(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "bgp", "applied.json")
	if err := bgpdaemon.WriteApplied(path, bgpdaemon.AppliedConfig{Global: bgpdaemon.AppliedGlobal{ASN: 64500, RouterID: "198.18.31.2"}}); err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(dir, render.BGPUnitName)
	if err := os.WriteFile(unit, []byte("# Managed by routerd. Do not edit by hand.\n[Service]\nExecStart=/usr/sbin/routerd-bgp\n"), 0644); err != nil {
		t.Fatal(err)
	}
	store := mapStore{
		api.NetAPIVersion + "/BGPRouter/edge":      {"phase": "Reconverging"},
		api.NetAPIVersion + "/BGPPeer/independent": {"phase": "Reconverging"},
		"other.invalid/v1/BGPPeer/independent":     {"phase": "Pending"},
	}
	stopped := false
	fib := &removalFIB{stopped: &stopped}
	c := SystemdUnitController{Router: &api.Router{Metadata: api.ObjectMeta{Name: "empty"}}, Store: store, SystemdSystemDir: dir, StateDir: filepath.Join(dir, "state"), BGPStatePath: path,
		BGPController: &bgpcontroller.Controller{Store: store, Daemon: manageddaemon.Spec{StatePath: path}, FIB: fib}}
	failStop := true
	c.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "systemctl" && len(args) > 0 && args[0] == "disable" && args[len(args)-1] == render.BGPUnitName {
			if failStop {
				return nil, errors.New("supervisor stop refused")
			}
			stopped = true
		}
		if name == "systemctl" && len(args) > 0 && args[0] == "is-active" {
			return nil, errors.New("inactive")
		}
		return []byte("ok"), nil
	}
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("failed stop was hidden")
	}
	if fib.clears != 0 || store.ObjectStatus(api.NetAPIVersion, "BGPPeer", "independent")["phase"] != "Reconverging" {
		t.Fatal("failed native removal retired public status")
	}
	failStop = false
	retiring := &bgpStatusDeleteStore{mapStore: store, fail: true}
	c.Store = retiring
	if err := c.Reconcile(t.Context()); err == nil {
		t.Fatal("status deletion failure hidden")
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatalf("native unit not removed before status retirement: %v", err)
	}
	if store.ObjectStatus(api.SystemAPIVersion, "ServiceUnit", render.BGPUnitName)["phase"] != "Removed" {
		t.Fatal("retry acknowledgement lost")
	}
	retiring.fail = false
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !stopped || fib.clears != 1 {
		t.Fatalf("missing acknowledged native removal: stop=%v clears=%d", stopped, fib.clears)
	}
	for _, id := range []string{api.NetAPIVersion + "/BGPRouter/edge", api.NetAPIVersion + "/BGPPeer/independent"} {
		if v, ok := store[id]; ok {
			t.Errorf("withdrawn BGP status retained: %s %v", id, v)
		}
	}
	if _, ok := store["other.invalid/v1/BGPPeer/independent"]; !ok {
		t.Fatal("foreign API status removed")
	}
}

func TestEmptyRouterRetiresLeaseSyncStatusesWithoutDeletingLeases(t *testing.T) {
	store := mapStore{}
	kinds := []string{"DHCPv4ServerLeaseSync", "DHCPv6ServerLeaseSync", "DHCPv6PrefixDelegationLeaseSync"}
	for _, k := range kinds {
		store[api.NetAPIVersion+"/"+k+"/leases"] = map[string]any{"phase": "Error", "reason": "SyncFailed"}
	}
	store[api.NetAPIVersion+"/DHCPv4Server/lan"] = map[string]any{"phase": "Applied"}
	store["other.invalid/v1/DHCPv4ServerLeaseSync/leases"] = map[string]any{"phase": "Pending"}
	lease := filepath.Join(t.TempDir(), "lease.db")
	if err := os.WriteFile(lease, []byte("retained lease data"), 0600); err != nil {
		t.Fatal(err)
	}
	c := FileSyncController{Router: &api.Router{}, Store: store, Command: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("removed sync ran a transfer")
		return nil, nil
	}}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, k := range kinds {
		if v, ok := store[api.NetAPIVersion+"/"+k+"/leases"]; ok {
			t.Errorf("withdrawn sync status retained: %s %v", k, v)
		}
	}
	if _, ok := store[api.NetAPIVersion+"/DHCPv4Server/lan"]; !ok {
		t.Fatal("different kind removed")
	}
	if _, ok := store["other.invalid/v1/DHCPv4ServerLeaseSync/leases"]; !ok {
		t.Fatal("foreign API removed")
	}
	if data, err := os.ReadFile(lease); err != nil || string(data) != "retained lease data" {
		t.Fatalf("lease data changed: %q %v", data, err)
	}
}

func TestCanceledLeaseSyncDoesNotPublishCurrentFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "leases")
	if err := os.WriteFile(path, []byte("lease"), 0600); err != nil {
		t.Fatal(err)
	}
	store := mapStore{api.NetAPIVersion + "/DHCPv4ServerLeaseSync/leases": {"phase": "Synced"}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := FileSyncController{Store: store, Command: func(context.Context, string, ...string) ([]byte, error) { cancel(); return nil, context.Canceled }}
	job := fileSyncJob{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync", Name: "leases", Sources: []fileSyncSource{{Path: path, Required: true}}, Targets: []fileSyncTarget{{Host: "198.18.30.3"}}}
	if err := c.reconcileJob(ctx, job); !errors.Is(err, context.Canceled) {
		t.Errorf("generation cancellation not returned: %v", err)
	}
	if s := store.ObjectStatus(api.NetAPIVersion, job.Kind, job.Name); s["phase"] != "Synced" {
		t.Errorf("canceled old generation published current error: %v", s)
	}
}

func TestLeaseSyncWithdrawalPreservesWhenFalseDeclaration(t *testing.T) {
	const kind = "DHCPv4ServerLeaseSync"
	store := mapStore{api.NetAPIVersion + "/" + kind + "/leases": {"phase": "Pending", "reason": "WhenFalse"}}
	declared := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: kind}, Metadata: api.ObjectMeta{Name: "leases"}}}}}
	c := FileSyncController{Router: &api.Router{}, DeclaredRouter: declared, Store: store}
	if err := c.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s := store.ObjectStatus(api.NetAPIVersion, kind, "leases"); s["reason"] != "WhenFalse" {
		t.Fatalf("disabled declaration retired: %v", s)
	}
}

func TestBGPWithdrawalPreservesWhenFalseDeclaration(t *testing.T) {
	store := mapStore{api.NetAPIVersion + "/BGPRouter/edge": {"phase": "Pending", "reason": "WhenFalse"}, api.NetAPIVersion + "/BGPPeer/peer": {"phase": "Pending", "reason": "WhenFalse"}, api.SystemAPIVersion + "/ServiceUnit/" + render.BGPUnitName: {"phase": "Removed"}}
	declared := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPRouter"}, Metadata: api.ObjectMeta{Name: "edge"}}, {TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "BGPPeer"}, Metadata: api.ObjectMeta{Name: "peer"}}}}}
	c := SystemdUnitController{Router: &api.Router{}, DeclaredRouter: declared, Store: store}
	for _, kind := range []string{"BGPRouter", "BGPPeer"} {
		if err := c.cleanupRemovedSystemdResourceStatuses(kind, func(string) string { return render.BGPUnitName }); err != nil {
			t.Fatal(err)
		}
	}
	if store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "edge")["reason"] != "WhenFalse" || store.ObjectStatus(api.NetAPIVersion, "BGPPeer", "peer")["reason"] != "WhenFalse" {
		t.Fatal("disabled BGP declaration retired")
	}
}

func TestBGPWithdrawalDryRunKeepsObservedStatus(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		store := mapStore{api.NetAPIVersion + "/BGPRouter/edge": {"phase": "Reconverging"}, api.SystemAPIVersion + "/ServiceUnit/" + render.BGPUnitName: {"phase": "Removed", "dryRun": !dryRun}}
		c := SystemdUnitController{Router: &api.Router{}, Store: store, DryRun: dryRun}
		if err := c.cleanupRemovedSystemdResourceStatuses("BGPRouter", func(string) string { return render.BGPUnitName }); err != nil {
			t.Fatal(err)
		}
		if status := store.ObjectStatus(api.NetAPIVersion, "BGPRouter", "edge"); status["phase"] != "Reconverging" {
			t.Fatalf("dry-run removal retired live observation (controller dryRun=%v): %v", dryRun, status)
		}
	}
}
