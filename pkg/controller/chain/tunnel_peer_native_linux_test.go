// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"encoding/json"
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

func TestTunnelNativeLinuxPeerLifecycle(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires ROUTERD_RUN_NETNS=1 in an isolated network namespace")
	}
	currentNS, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	hostNS := os.Getenv("ROUTERD_HOST_NETNS")
	if !strings.HasPrefix(hostNS, "net:[") || hostNS == currentNS {
		t.Fatal("outer network namespace isolation is unverified")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	defer cancel()
	peer := exec.CommandContext(ctx, "unshare", "-n", "--", "sleep", "180")
	if err := peer.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- peer.Wait() }()
	stopped := false
	stopPeer := func() {
		if stopped {
			return
		}
		_ = peer.Process.Kill()
		select {
		case <-done:
			stopped = true
			t.Log("independent tunnel peer exited and was reaped")
		case <-time.After(5 * time.Second):
			t.Error("independent tunnel peer did not acknowledge exit")
		}
	}
	t.Cleanup(stopPeer)
	peerPath := fmt.Sprintf("/proc/%d/ns/net", peer.Process.Pid)
	deadline := time.Now().Add(3 * time.Second)
	for {
		namespace, readErr := os.Readlink(peerPath)
		if readErr == nil && namespace != currentNS {
			t.Logf("isolated tunnel namespaces: host=%s router=%s peer=%s", hostNS, currentNS, namespace)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("independent peer namespace did not become ready: %v", readErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	peerNS, err := os.Open(peerPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peerNS.Close() })
	run := func(name string, args ...string) []byte {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", name, args, err, out)
		}
		return out
	}
	peerCommand := func(name string, args ...string) *exec.Cmd {
		command := exec.CommandContext(ctx, "nsenter", append([]string{"--net=/proc/self/fd/3", "--", name}, args...)...)
		command.ExtraFiles = []*os.File{peerNS}
		return command
	}
	peerRun := func(name string, args ...string) []byte {
		out, err := peerCommand(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("independent peer %s %v: %v: %s", name, args, err, out)
		}
		return out
	}
	run("ip", "link", "add", "name", "tn-proof-u", "type", "veth", "peer", "name", "tn-peer-u")
	run("ip", "link", "set", "dev", "tn-peer-u", "netns", strconv.Itoa(peer.Process.Pid))
	run("ip", "address", "add", "198.18.89.1/30", "dev", "tn-proof-u")
	run("ip", "link", "set", "dev", "tn-proof-u", "up")
	peerRun("ip", "address", "add", "198.18.89.2/30", "dev", "tn-peer-u")
	peerRun("ip", "link", "set", "dev", "tn-peer-u", "up")
	peerRun("ip", "link", "set", "dev", "lo", "up")
	peerRun("ip", "address", "add", "192.0.2.2/32", "dev", "lo")
	for _, mode := range []string{"ipip", "gre", "fou", "gue"} {
		t.Run(mode, func(t *testing.T) {
			run := func(name string, args ...string) []byte {
				out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
				if err != nil {
					t.Fatalf("%s %v: %v: %s", name, args, err, out)
				}
				return out
			}
			peerRun := func(name string, args ...string) []byte {
				out, err := peerCommand(name, args...).CombinedOutput()
				if err != nil {
					t.Fatalf("independent peer %s %v: %v: %s", name, args, err, out)
				}
				return out
			}
			defer func() {
				_, _ = peerCommand("ip", "link", "delete", "dev", "tn-peer").CombinedOutput()
				_, _ = exec.CommandContext(ctx, "ip", "link", "delete", "dev", "tn-proof").CombinedOutput()
			}()
			spec := api.TunnelInterfaceSpec{Mode: mode, LocalFrom: api.StatusValueSourceSpec{Resource: "DHCPv4Client/underlay", Field: "currentAddress"}, Remote: "198.18.89.2", Address: "10.253.89.1/30", MTU: 1400, TrustedUnderlay: true}
			kind := tunnelNativeKind(mode)
			args := []string{"link", "add", "name", "tn-peer", "type", kind, "local", "198.18.89.2", "remote", "198.18.89.1", "ttl", "64"}
			if mode == "gre" {
				spec.Key = 42
				args = append(args, "key", "42")
			} else if mode == "fou" || mode == "gue" {
				spec.EncapSport, spec.EncapDport = 5555, 5556
				listener := []string{"fou", "add", "port", "5556"}
				if mode == "fou" {
					listener = append(listener, "ipproto", "4")
				} else {
					listener = append(listener, "gue")
				}
				peerRun("ip", listener...)
				defer func() { _, _ = peerCommand("ip", "fou", "del", "port", "5556").CombinedOutput() }()
				args = append(args, "encap", mode, "encap-sport", "5556", "encap-dport", "5555")
			}
			peerRun("ip", args...)
			peerRun("ip", "address", "add", "10.253.89.2/30", "dev", "tn-peer")
			peerRun("ip", "link", "set", "dev", "tn-peer", "mtu", "1400", "up")
			peerRun("ip", "route", "add", "192.0.2.1/32", "dev", "tn-peer")
			store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			setSource := func(expired bool) {
				phase, address := "Bound", "198.18.89.1/30"
				if expired {
					phase, address = "Expired", ""
				}
				if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "underlay", map[string]any{"phase": phase, "currentAddress": address}); err != nil {
					t.Fatal(err)
				}
			}
			setSource(false)
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
				{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"}, Metadata: api.ObjectMeta{Name: "tn-proof"}, Spec: spec},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "inside-source"}, Spec: api.IPv4StaticAddressSpec{Interface: "tn-proof", Address: "192.0.2.1/32"}},
				{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "inside-route"}, Spec: api.IPv4RouteSpec{Destination: "192.0.2.2/32", Device: "tn-proof"}},
			}}}
			parent := TunnelInterfaceController{Router: router, Store: eventedStore{Store: store, Router: router}}
			applies := 0
			parent.ApplyNative = func(ctx context.Context, record tunnelNativeReceipt) error {
				applies++
				return applyTunnelNativePlan(ctx, record)
			}
			address := IPv4StaticAddressController{Router: router, Store: store}
			route := IPv4RouteController{Router: router, Store: store}
			acquire := func() tunnelNativeReceipt {
				if err := parent.Reconcile(ctx); err != nil {
					t.Fatalf("native %s acquisition: %v", mode, err)
				}
				records, err := parent.tunnelNativeReceipts()
				if err != nil || len(records) != 1 || records[0].Pending || records[0].Index < 1 {
					t.Fatalf("native %s receipt not acknowledged: %+v %v", mode, records, err)
				}
				if err := address.Reconcile(ctx); err != nil {
					t.Fatal(err)
				}
				if err := route.reconcile(ctx); err != nil {
					t.Fatal(err)
				}
				status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv4StaticAddress", "inside-source")
				child, decodeErr := staticReceipt(status)
				if err != nil || decodeErr != nil || child == nil || child.ParentToken != "tunnel:"+records[0].Token || child.IfIndex != records[0].Index || child.Scope != records[0].Scope {
					t.Fatalf("native child did not consume exact %s parent: %+v %v %v", mode, child, err, decodeErr)
				}
				return records[0]
			}
			record := acquire()
			if err := parent.Reconcile(ctx); err != nil || applies != 1 {
				t.Fatalf("unchanged native %s reconciliation mutated its owner: applies=%d err=%v", mode, applies, err)
			}
			var counters []struct {
				Stats struct {
					RX struct{ Packets uint64 } `json:"rx"`
				} `json:"stats64"`
			}
			if err := json.Unmarshal(peerRun("ip", "-j", "-s", "link", "show", "dev", "tn-peer"), &counters); err != nil || len(counters) != 1 {
				t.Fatalf("independent peer counters unavailable: %v", err)
			}
			before := counters[0].Stats.RX.Packets
			run("ping", "-c", "2", "-W", "2", "-I", "192.0.2.1", "192.0.2.2")
			if err := json.Unmarshal(peerRun("ip", "-j", "-s", "link", "show", "dev", "tn-peer"), &counters); err != nil || len(counters) != 1 || counters[0].Stats.RX.Packets < before+2 {
				t.Fatalf("independent %s peer did not receive encapsulated IPv4: %+v err=%v", mode, counters, err)
			}
			t.Logf("independent %s peer received IPv4: packets before=%d after=%d", mode, before, counters[0].Stats.RX.Packets)
			setSource(true)
			address.Router, route.Router = &api.Router{}, &api.Router{}
			if err := route.reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			if err := address.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			if err := parent.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			if link, err := observeTunnelNativeLink(ctx, "tn-proof"); err != nil || link.Exists {
				t.Fatalf("expired %s source retained native owner: %+v %v", mode, link, err)
			}
			setSource(false)
			address.Router, route.Router = router, router
			reacquired := acquire()
			if reacquired.Token == record.Token {
				t.Fatal("withdrawn tunnel token was reused")
			}
			run("ping", "-c", "1", "-W", "2", "-I", "192.0.2.1", "192.0.2.2")
			address.Router, route.Router = &api.Router{}, &api.Router{}
			if err := route.reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			if err := address.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			parent.Router = &api.Router{}
			if err := parent.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			if records, err := parent.tunnelNativeReceipts(); err != nil || len(records) != 0 {
				t.Fatalf("native withdrawal retained %s receipt: %+v %v", mode, records, err)
			}
		})
	}
	stopPeer()
}
