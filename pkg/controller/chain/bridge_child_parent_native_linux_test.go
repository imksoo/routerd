// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// This opt-in smoke exercises native Bridge/address ownership and restart
// recovery only. networkctl is deliberately excluded; no peer or forwarding
// result is implied by creating and withdrawing an address on this bridge.
func TestBridgeParentNativeLinuxAddressLifecycle(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires ROUTERD_RUN_NETNS=1 in an isolated network namespace")
	}
	currentNS, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatalf("cannot verify the current network namespace: %v", err)
	}
	hostNS := strings.TrimSpace(os.Getenv("ROUTERD_HOST_NETNS"))
	if hostNS == "" {
		hostNS, err = os.Readlink("/proc/1/ns/net")
		if err != nil {
			t.Fatalf("cannot verify an isolated network namespace: %v", err)
		}
	}
	if !strings.HasPrefix(hostNS, "net:[") || !strings.HasSuffix(hostNS, "]") || currentNS == hostNS {
		t.Fatalf("network namespace isolation is unverified: current=%q host=%q", currentNS, hostNS)
	}
	t.Logf("isolated network namespace: current=%s host=%s", currentNS, hostNS)

	directory := t.TempDir()
	database := filepath.Join(directory, "state.db")
	store, err := routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if store != nil {
			_ = store.Close()
		}
	})
	stp := false
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "parent"}, Spec: api.BridgeSpec{IfName: "br-proof", STP: &stp}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "child"}, Spec: api.IPv4StaticAddressSpec{Interface: "parent", Address: "198.18.0.1/24"}},
	}}}
	nativeCreates := 0
	var nativeCommands []string
	command := func(ctx context.Context, name string, args ...string) error {
		if name == "networkctl" {
			return nil
		}
		if name == "ip" && len(args) > 1 && args[0] == "link" && args[1] == "add" {
			nativeCreates++
		}
		nativeCommands = append(nativeCommands, name+" "+strings.Join(args, " "))
		output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s %v: %w: %s", name, args, err, output)
		}
		return nil
	}
	parent := BridgeController{Router: router, Store: store, NetworkdDir: filepath.Join(directory, "networkd"), Command: command}
	if err := parent.Reconcile(t.Context()); err != nil {
		link, linkErr := exec.CommandContext(t.Context(), "ip", "-d", "link", "show", "br-proof").CombinedOutput()
		alias, aliasErr := os.ReadFile("/sys/class/net/br-proof/ifalias")
		owners, ownersErr := parent.readBridgeOwners()
		t.Logf("native commands: %v", nativeCommands)
		t.Logf("ip -d link show br-proof: %s error=%v", link, linkErr)
		t.Logf("sysfs ifalias: %q error=%v", alias, aliasErr)
		t.Logf("Bridge ownership receipts: %+v error=%v", owners, ownersErr)
		t.Fatalf("native Bridge acquisition: %v", err)
	}
	owners, err := parent.readBridgeOwners()
	if err != nil || len(owners) != 1 || !owners[0].Activated || owners[0].Withdrawing || owners[0].IfIndex < 1 {
		t.Fatalf("native parent receipt: %+v %v", owners, err)
	}
	owner := owners[0]
	parentReceipt, err := os.ReadFile(parent.bridgeOwnerPath(owner.Resource))
	if err != nil {
		t.Fatal(err)
	}
	child := IPv4StaticAddressController{Router: router, Store: store, Bridges: &parent}
	if err := child.Reconcile(t.Context()); err != nil {
		t.Fatalf("native child acquisition: %v", err)
	}
	output, err := exec.CommandContext(t.Context(), "ip", "-o", "-4", "address", "show", "dev", "br-proof").CombinedOutput()
	if err != nil || !strings.Contains(string(output), "198.18.0.1/24") {
		t.Fatalf("native child address is absent: %s %v", output, err)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "child")
	if err != nil {
		t.Fatal(err)
	}
	childReceipt, err := staticReceipt(status)
	if err != nil || childReceipt == nil || childReceipt.Pending || childReceipt.IfIndex != owner.IfIndex || childReceipt.Scope != owner.Scope || childReceipt.ParentToken != "bridge:"+owner.Token {
		t.Fatalf("native child receipt: %+v %v", childReceipt, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = routerstate.OpenSQLite(database)
	if err != nil {
		t.Fatal(err)
	}
	parent = BridgeController{Router: router, Store: store, NetworkdDir: parent.NetworkdDir, Command: command}
	child = IPv4StaticAddressController{Router: router, Store: store, Bridges: &parent}
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatalf("restarted native parent: %v", err)
	}
	if err := child.Reconcile(t.Context()); err != nil {
		t.Fatalf("restarted native child: %v", err)
	}
	afterParent, err := os.ReadFile(parent.bridgeOwnerPath(owner.Resource))
	if err != nil || string(afterParent) != string(parentReceipt) || nativeCreates != 1 {
		t.Fatalf("restart replaced the native parent: creates=%d err=%v", nativeCreates, err)
	}
	status, err = store.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "child")
	if err != nil {
		t.Fatal(err)
	}
	afterChild, err := staticReceipt(status)
	if err != nil || afterChild == nil || *afterChild != *childReceipt {
		t.Fatalf("restart replaced the acknowledged child receipt: before=%+v after=%+v err=%v", childReceipt, afterChild, err)
	}
	// Remove the declarations before child withdrawal so cleanup must use the
	// recorded parent proof rather than reconstructing authority from intent.
	child.Router = &api.Router{}
	if err := child.Reconcile(t.Context()); err != nil {
		t.Fatalf("recorded native child withdrawal: %v", err)
	}
	output, err = exec.CommandContext(t.Context(), "ip", "-o", "-4", "address", "show", "dev", "br-proof").CombinedOutput()
	if err != nil || strings.Contains(string(output), "198.18.0.1/24") {
		t.Fatalf("native child address remains after withdrawal: %s %v", output, err)
	}
	status, err = store.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "child")
	if err != nil {
		t.Fatal(err)
	}
	if receipt, err := staticReceipt(status); err != nil || receipt != nil {
		t.Fatalf("withdrawn native child retained an ownership receipt: %+v %v", receipt, err)
	}
	parent.Router = &api.Router{}
	if err := parent.Reconcile(t.Context()); err != nil {
		t.Fatalf("native parent withdrawal: %v", err)
	}
	if iface, err := parent.bridgeInterface("br-proof"); err != nil || iface != nil {
		t.Fatalf("native bridge remains after withdrawal: %+v %v", iface, err)
	}
	if owners, err := parent.readBridgeOwners(); err != nil || len(owners) != 0 {
		t.Fatalf("native parent receipt remains after withdrawal: %+v %v", owners, err)
	}
	if _, err := os.Stat(filepath.Join(parent.NetworkdDir, "30-routerd-br-proof.network")); !os.IsNotExist(err) {
		t.Fatalf("owned networkd artifact remains after withdrawal: %v", err)
	}
}
