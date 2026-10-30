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
	"github.com/imksoo/routerd/pkg/wireguard"
)

func TestWireGuardParentNativeLinuxPeerLifecycle(t *testing.T) {
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
		if err != nil && name == "wg" {
			t.Fatalf("WireGuard command %v failed: %v", args, err)
		}
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return out
	}
	// Own a direct child and a namespace descriptor, without host /run/netns
	// registrations. The peer has a distinct local routing table.
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
			t.Log("independent peer namespace process exited and was reaped")
		case <-time.After(5 * time.Second):
			t.Error("independent peer namespace process did not acknowledge exit")
		}
	})
	namespacePath := fmt.Sprintf("/proc/%d/ns/net", peer.Process.Pid)
	deadline := time.Now().Add(3 * time.Second)
	for {
		namespace, readErr := os.Readlink(namespacePath)
		if readErr == nil && namespace != currentNS {
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
	peerRun := func(input string, name string, args ...string) []byte {
		command := exec.CommandContext(ctx, "nsenter", append([]string{"--net=/proc/self/fd/3", "--", name}, args...)...)
		command.ExtraFiles = []*os.File{peerNS}
		if input != "" {
			command.Stdin = strings.NewReader(input)
		}
		out, err := command.CombinedOutput()
		if err != nil && name == "wg" {
			t.Fatalf("independent peer WireGuard command %v failed: %v", args, err)
		}
		if err != nil {
			t.Fatalf("independent peer %s %v: %v: %s", name, args, err, out)
		}
		return out
	}
	run("ip", "link", "add", "name", "wg-proof-u", "type", "veth", "peer", "name", "wg-peer-u")
	run("ip", "link", "set", "dev", "wg-peer-u", "netns", strconv.Itoa(peer.Process.Pid))
	run("ip", "address", "add", "198.18.0.1/30", "dev", "wg-proof-u")
	run("ip", "link", "set", "dev", "wg-proof-u", "up")
	peerRun("", "ip", "address", "add", "198.18.0.2/30", "dev", "wg-peer-u")
	peerRun("", "ip", "link", "set", "dev", "wg-peer-u", "up")
	peerRun("", "ip", "link", "set", "dev", "lo", "up")
	private, err := wireguard.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	public, err := wireguard.PublicKeyFromPrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	peerPrivate, err := wireguard.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	peerPublic, err := wireguard.PublicKeyFromPrivateKey(peerPrivate)
	if err != nil {
		t.Fatal(err)
	}
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
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "parent"}, Spec: api.WireGuardInterfaceSpec{IfName: "wg-proof", PrivateKey: private, MTU: 1420}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardPeer"}, Metadata: api.ObjectMeta{Name: "peer"}, Spec: api.WireGuardPeerSpec{Interface: "parent", PublicKey: peerPublic, AllowedIPs: []string{"192.0.2.2/32"}, Endpoint: "198.18.0.2:51821"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "address"}, Spec: api.IPv4StaticAddressSpec{Interface: "parent", Address: "192.0.2.1/32"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "route"}, Spec: api.IPv4RouteSpec{Destination: "192.0.2.2/32", Device: "wg-proof"}},
	}}}
	parent := WireGuardController{Router: router, Store: store}
	if err := parent.Reconcile(ctx); err != nil {
		t.Fatalf("native WireGuard acquisition: %v", err)
	}
	records, err := parent.readNativeWireGuard()
	if err != nil || len(records) != 1 || records[0].Pending || records[0].IfIndex < 1 {
		t.Fatalf("native ownership receipt is not acknowledged: %v", err)
	}
	record := records[0]
	address := IPv4StaticAddressController{Router: router, Store: store, WireGuard: &parent}
	if err := address.Reconcile(ctx); err != nil {
		t.Fatalf("native child address: %v", err)
	}
	route := IPv4RouteController{Router: router, Store: store, WireGuard: &parent}
	if err := route.reconcile(ctx); err != nil {
		t.Fatalf("native child route: %v", err)
	}
	status, err := parent.interfaceStatus(ctx, "wg-proof")
	if err != nil || status.ListenPort < 1 {
		t.Fatalf("native WireGuard has no observed UDP port: %v", err)
	}
	peerRun("", "ip", "link", "add", "name", "wg-peer", "type", "wireguard")
	peerConfig := fmt.Sprintf("[Interface]\nPrivateKey = %s\nListenPort = 51821\n\n[Peer]\nPublicKey = %s\nAllowedIPs = 192.0.2.1/32\nEndpoint = 198.18.0.1:%d\n", peerPrivate, public, status.ListenPort)
	peerRun(peerConfig, "wg", "setconf", "wg-peer", "/dev/stdin")
	peerRun("", "ip", "address", "add", "192.0.2.2/32", "dev", "wg-peer")
	peerRun("", "ip", "link", "set", "dev", "wg-peer", "up")
	peerRun("", "ip", "route", "add", "192.0.2.1/32", "dev", "wg-peer")
	run("ping", "-c", "2", "-W", "3", "-I", "192.0.2.1", "192.0.2.2")
	checkTraffic := func(name string, dump []byte) {
		status, err := wireguard.ParseInterfaceDump(name, dump)
		if err != nil || len(status.Peers) != 1 {
			t.Fatalf("%s peer observation is invalid: %v", name, err)
		}
		peer := status.Peers[0]
		if peer.LatestHandshake.IsZero() || peer.TransferRxBytes <= 0 || peer.TransferTxBytes <= 0 {
			t.Fatalf("%s has no acknowledged encrypted exchange: handshake=%v rx=%d tx=%d", name, peer.LatestHandshake, peer.TransferRxBytes, peer.TransferTxBytes)
		}
		t.Logf("%s encrypted peer exchange: handshake=%s rx=%d tx=%d", name, peer.LatestHandshake.UTC().Format(time.RFC3339), peer.TransferRxBytes, peer.TransferTxBytes)
	}
	checkTraffic("wg-proof", run("wg", "show", "wg-proof", "dump"))
	checkTraffic("wg-peer", peerRun("", "wg", "show", "wg-peer", "dump"))
	run("ip", "link", "set", "dev", "wg-proof", "down")
	if err := parent.Reconcile(ctx); err != nil {
		t.Fatalf("owned native WireGuard down-state recovery: %v", err)
	}
	recovered, err := parent.observeNativeWireGuard(ctx, "wg-proof")
	if err != nil || !recovered.Exists || !recovered.Up || recovered.IfIndex != record.IfIndex || recovered.Token != record.Token {
		t.Fatalf("native recovery replaced or failed to restore the owned parent: %v", err)
	}
	run("ping", "-c", "1", "-W", "3", "-I", "192.0.2.1", "192.0.2.2")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	parent = WireGuardController{Router: router, Store: store}
	address = IPv4StaticAddressController{Router: router, Store: store, WireGuard: &parent}
	route = IPv4RouteController{Router: router, Store: store, WireGuard: &parent}
	if err := parent.Reconcile(ctx); err != nil {
		t.Fatalf("native parent reopen: %v", err)
	}
	if err := address.Reconcile(ctx); err != nil {
		t.Fatalf("native child address reopen: %v", err)
	}
	if err := route.reconcile(ctx); err != nil {
		t.Fatalf("native child route reopen: %v", err)
	}
	reopened, err := parent.readNativeWireGuard()
	if err != nil || len(reopened) != 1 || !sameWireGuardNativeRecord(record, reopened[0]) {
		t.Fatalf("native parent receipt changed after reopen: %v", err)
	}
	run("ping", "-c", "1", "-W", "3", "-I", "192.0.2.1", "192.0.2.2")
	route.Router, address.Router = &api.Router{}, &api.Router{}
	if err := route.reconcile(ctx); err != nil {
		t.Fatalf("native recorded child route withdrawal: %v", err)
	}
	if err := address.Reconcile(ctx); err != nil {
		t.Fatalf("native recorded child address withdrawal: %v", err)
	}
	if out := run("ip", "-o", "-4", "address", "show", "dev", "wg-proof"); strings.Contains(string(out), "192.0.2.1/32") {
		t.Fatal("child address remains after withdrawal")
	}
	if out := run("ip", "-4", "route", "show", "192.0.2.2/32"); len(strings.TrimSpace(string(out))) != 0 {
		t.Fatal("child route remains after withdrawal")
	}
	parent.Router = &api.Router{}
	if err := parent.Reconcile(ctx); err != nil {
		t.Fatalf("native parent withdrawal: %v", err)
	}
	link, err := parent.observeNativeWireGuard(ctx, "wg-proof")
	if err != nil || link.Exists {
		t.Fatalf("native parent remains after withdrawal: %v", err)
	}
	if records, err := parent.readNativeWireGuard(); err != nil || len(records) != 0 {
		t.Fatalf("native parent receipt remains after withdrawal: %v", err)
	}
}
