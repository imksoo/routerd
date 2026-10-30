// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestVRFParentNativeLinuxPeerLifecycle(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires ROUTERD_RUN_NETNS=1 in an isolated network namespace")
	}
	currentNS, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Skipf("cannot observe the current network namespace: %v", err)
	}
	hostNS := strings.TrimSpace(os.Getenv("ROUTERD_HOST_NETNS"))
	if hostNS == "" {
		hostNS, err = os.Readlink("/proc/1/ns/net")
		if err != nil {
			t.Skipf("cannot verify network namespace isolation: %v", err)
		}
	}
	if !strings.HasPrefix(hostNS, "net:[") || !strings.HasSuffix(hostNS, "]") || currentNS == hostNS {
		t.Skipf("network namespace isolation is unverified: current=%q host=%q", currentNS, hostNS)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
	defer cancel()
	run := func(name string, args ...string) []byte {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return out
	}
	peer := exec.CommandContext(ctx, "unshare", "-n", "--", "sleep", "120")
	if err := peer.Start(); err != nil {
		t.Fatal(err)
	}
	peerDone := make(chan error, 1)
	go func() { peerDone <- peer.Wait() }()
	t.Cleanup(func() {
		_ = peer.Process.Kill()
		select {
		case <-peerDone:
			t.Log("independent VRF peer namespace process exited and was reaped")
		case <-time.After(5 * time.Second):
			t.Error("independent VRF peer namespace process did not acknowledge exit")
		}
	})
	namespacePath := fmt.Sprintf("/proc/%d/ns/net", peer.Process.Pid)
	deadline := time.Now().Add(3 * time.Second)
	for {
		namespace, readErr := os.Readlink(namespacePath)
		if readErr == nil && namespace != currentNS {
			t.Logf("isolated VRF namespaces: host=%s router=%s peer=%s", hostNS, currentNS, namespace)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("independent peer namespace did not become ready: %v", readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	peerNS, err := os.Open(namespacePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peerNS.Close() })
	peerRun := func(name string, args ...string) []byte {
		command := exec.CommandContext(ctx, "nsenter", append([]string{"--net=/proc/self/fd/3", "--", name}, args...)...)
		command.ExtraFiles = []*os.File{peerNS}
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("independent peer %s %v: %v: %s", name, args, err, out)
		}
		return out
	}
	run("ip", "link", "add", "name", "vrf-proof-p", "type", "veth", "peer", "name", "vrf-peer-p")
	run("ip", "link", "set", "dev", "vrf-peer-p", "netns", strconv.Itoa(peer.Process.Pid))
	run("ip", "link", "set", "dev", "vrf-proof-p", "up")
	peerRun("ip", "address", "add", "198.18.0.2/30", "dev", "vrf-peer-p")
	peerRun("ip", "link", "set", "dev", "vrf-peer-p", "up")
	peerRun("ip", "link", "set", "dev", "lo", "up")
	database := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			_ = store.Close()
		}
	})
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "port"}, Spec: api.InterfaceSpec{IfName: "vrf-proof-p"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "parent"}, Spec: api.VRFSpec{IfName: "vrf-proof", RouteTable: 101, Members: []string{"port"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "address"}, Spec: api.IPv4StaticAddressSpec{Interface: "parent", Address: "192.0.2.1/32"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "port-address"}, Spec: api.IPv4StaticAddressSpec{Interface: "port", Address: "198.18.0.1/30"}},
		// The current route contract selects the main table. Exercise its direct
		// VRF ownership proof separately from the connected table-101 peer path.
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "proof-route"}, Spec: api.IPv4RouteSpec{Destination: "203.0.113.77/32", Device: "vrf-proof"}},
	}}}
	parent := VRFController{Router: router, Store: store}
	if err := parent.Reconcile(ctx); err != nil {
		t.Fatalf("native VRF acquisition: %v", err)
	}
	records, err := parent.readReceipts()
	if err != nil || len(records) != 1 || records[0].Pending || records[0].Index < 1 {
		t.Fatalf("native VRF ownership receipt is not acknowledged: records=%+v err=%v", records, err)
	}
	record := records[0]
	checkNative := func() {
		link, err := parent.native().Observe(ctx, record.Plan.Name)
		if err != nil || !link.Exists || !link.Up || link.Index != record.Index || link.Kind != "vrf" || link.Table != 101 || link.Scope != record.Scope || link.Token != record.Token {
			t.Fatalf("native VRF identity differs from receipt: %+v err=%v", link, err)
		}
		port, err := parent.native().Observe(ctx, "vrf-proof-p")
		if err != nil || !port.Exists || port.Index != record.Ports[0].Index || port.Master != record.Index || port.Scope != record.Scope {
			t.Fatalf("native member association differs from receipt: %+v err=%v", port, err)
		}
	}
	checkNative()
	address := IPv4StaticAddressController{Router: router, Store: store, VRFs: &parent}
	if err := address.Reconcile(ctx); err != nil {
		t.Fatalf("native VRF child addresses: %v", err)
	}
	route := IPv4RouteController{Router: router, Store: store, VRFs: &parent}
	if err := route.reconcile(ctx); err != nil {
		t.Fatalf("native VRF direct child route: %v", err)
	}
	checkProof := func() {
		status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "address")
		if err != nil {
			t.Fatal(err)
		}
		child, err := staticReceipt(status)
		if err != nil || child == nil || child.Pending || child.ParentToken != "vrf:"+record.Token || child.IfIndex != record.Index || child.Scope != record.Scope {
			t.Fatalf("native VRF child address lacks exact proof: %+v err=%v", child, err)
		}
		status, err = store.ReadObjectStatus(api.NetAPIVersion, "IPv4Route", "proof-route")
		if err != nil {
			t.Fatal(err)
		}
		childRoute, err := ipv4RouteReceiptFromStatus(status)
		if err != nil || childRoute == nil || childRoute.Pending || childRoute.ParentToken != "vrf:"+record.Token || childRoute.IfIndex != record.Index || childRoute.Scope != record.Scope {
			t.Fatalf("native VRF child route lacks exact proof: %+v err=%v", childRoute, err)
		}
	}
	checkProof()
	checkPeer := func() {
		connected := string(run("ip", "route", "show", "table", "101"))
		if !strings.Contains(connected, "198.18.0.0/30 dev vrf-proof-p") {
			t.Fatalf("VRF member connected route is absent from table101: %s", connected)
		}
		if main := string(run("ip", "route", "show", "table", "main")); strings.Contains(main, "198.18.0.0/30") {
			t.Fatalf("VRF member connected route leaked into the main table: %s", main)
		}
		t.Logf("VRF-bound independent peer communication: %s", strings.TrimSpace(string(run("ping", "-n", "-c", "1", "-W", "2", "-I", "vrf-proof", "198.18.0.2"))))
	}
	checkPeer()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	parent = VRFController{Router: router, Store: store}
	address = IPv4StaticAddressController{Router: router, Store: store, VRFs: &parent}
	route = IPv4RouteController{Router: router, Store: store, VRFs: &parent}
	if err := parent.Reconcile(ctx); err != nil {
		t.Fatalf("native VRF reopen: %v", err)
	}
	if err := address.Reconcile(ctx); err != nil {
		t.Fatalf("native VRF child reopen: %v", err)
	}
	if err := route.reconcile(ctx); err != nil {
		t.Fatalf("native VRF route reopen: %v", err)
	}
	records, err = parent.readReceipts()
	if err != nil || len(records) != 1 || !sameVRFReceipt(record, records[0]) {
		t.Fatalf("reopen replaced the owned native generation: %+v err=%v", records, err)
	}
	checkNative()
	checkProof()
	checkPeer()
	run("ip", "link", "set", "dev", "vrf-proof", "down")
	if err := parent.Reconcile(ctx); err != nil {
		t.Fatalf("native VRF administrative-down recovery: %v", err)
	}
	if err := address.Reconcile(ctx); err != nil {
		t.Fatalf("native VRF recovery child addresses: %v", err)
	}
	if err := route.reconcile(ctx); err != nil {
		t.Fatalf("native VRF recovery child route: %v", err)
	}
	records, err = parent.readReceipts()
	if err != nil || len(records) != 1 || !sameVRFReceipt(record, records[0]) {
		t.Fatalf("administrative recovery replaced the owned VRF generation: %+v err=%v", records, err)
	}
	checkNative()
	checkProof()
	checkPeer()
	// Withdraw actual children before deleting the master. Physical member
	// addresses survive master deletion and must be removed by their own owner.
	empty := &api.Router{}
	parent.Router, address.Router, route.Router = empty, empty, empty
	if err := route.reconcile(ctx); err != nil {
		t.Fatalf("native VRF child route withdrawal: %v", err)
	}
	if err := address.Reconcile(ctx); err != nil {
		t.Fatalf("native VRF child address withdrawal: %v", err)
	}
	if err := parent.Reconcile(ctx); err != nil {
		t.Fatalf("native VRF withdrawal: %v", err)
	}
	link, err := parent.native().Observe(ctx, "vrf-proof")
	if err != nil || link.Exists || link.Scope != record.Scope {
		t.Fatalf("native VRF withdrawal lacks same-scope absence: %+v err=%v", link, err)
	}
	port, err := parent.native().Observe(ctx, "vrf-proof-p")
	if err != nil || !port.Exists || port.Master != 0 {
		t.Fatalf("withdrawal retained a member association: %+v err=%v", port, err)
	}
	if addresses := string(run("ip", "-4", "address", "show", "dev", "vrf-proof-p")); strings.Contains(addresses, "198.18.0.1/") {
		t.Fatalf("member child address survived withdrawal: %s", addresses)
	}
	if routes := string(run("ip", "route", "show", "table", "main")); strings.Contains(routes, "203.0.113.77") {
		t.Fatalf("direct child route survived withdrawal: %s", routes)
	}
	records, err = parent.readReceipts()
	if err != nil || len(records) != 0 {
		t.Fatalf("native withdrawal retained its receipt: %+v err=%v", records, err)
	}
	t.Logf("VRF native identity, table101 peer traffic, SQLite reopen, child withdrawal, and parent withdrawal verified in %s", currentNS)
}
