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
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	state "github.com/imksoo/routerd/pkg/state"
)

// The PD input is a finite stored lease; RA reception, SLAAC and IPv6 traffic
// use the actual kernel and production controllers, not a captured socket.
func TestVRFNativeLinuxDelegatedRA(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires explicit isolated network namespace")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	host := os.Getenv("ROUTERD_HOST_NETNS")
	if !strings.HasPrefix(host, "net:[") || current == host {
		t.Skip("network namespace isolation is unverified")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	run := func(command string, args ...string) string {
		out, err := exec.CommandContext(ctx, command, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v: %s", command, args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	newNamespace := func() (string, int) {
		peer := exec.CommandContext(ctx, "unshare", "-n", "sleep", "60")
		if err := peer.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = peer.Process.Kill(); _ = peer.Wait() })
		ns := fmt.Sprintf("/proc/%d/ns/net", peer.Process.Pid)
		deadline := time.Now().Add(3 * time.Second)
		for {
			id, err := os.Readlink(ns)
			if err == nil && id != current {
				return ns, peer.Process.Pid
			}
			if time.Now().After(deadline) {
				t.Fatal("peer namespace did not start")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	ns, clientPID := newNamespace()
	upstreamNS, upstreamPID := newNamespace()
	upstream := func(command string, args ...string) string {
		return run("nsenter", append([]string{"--net=" + upstreamNS, "--", command}, args...)...)
	}
	run("sysctl", "-qw", "net.ipv6.conf.all.forwarding=1")
	run("ip", "link", "add", "vrf-ra-wan", "type", "veth", "peer", "name", "vrf-ra-server")
	run("ip", "link", "set", "vrf-ra-server", "netns", fmt.Sprint(upstreamPID))
	run("ip", "link", "set", "vrf-ra-wan", "up")
	upstream("ip", "link", "set", "lo", "up")
	upstream("ip", "link", "set", "vrf-ra-server", "up")
	upstream("ip", "-6", "address", "add", "2001:db8:999::2/64", "dev", "vrf-ra-server")
	for _, prefix := range []string{"2001:db8:1200::/60", "2001:db8:1300::/60"} {
		upstream("ip", "-6", "route", "add", prefix, "via", "2001:db8:999::1", "dev", "vrf-ra-server")
	}
	client := func(command string, args ...string) string {
		return run("nsenter", append([]string{"--net=" + ns, "--", command}, args...)...)
	}
	run("ip", "link", "add", "vrf-ra-lan", "type", "veth", "peer", "name", "vrf-ra-client")
	run("ip", "link", "set", "vrf-ra-client", "netns", fmt.Sprint(clientPID))
	run("ip", "link", "set", "vrf-ra-lan", "up")
	client("ip", "link", "set", "lo", "up")
	client("sysctl", "-qw", "net.ipv6.conf.all.forwarding=0", "net.ipv6.conf.vrf-ra-client.accept_ra=2", "net.ipv6.conf.vrf-ra-client.autoconf=1")
	client("ip", "link", "set", "vrf-ra-client", "up")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "vrf-ra-lan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "vrf-ra-wan"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VRF"}, Metadata: api.ObjectMeta{Name: "tenant"}, Spec: api.VRFSpec{IfName: "vrf-ra", RouteTable: 101, Members: []string{"lan", "wan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: "wan-pd"}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6DelegatedAddress"}, Metadata: api.ObjectMeta{Name: "lan-v6"}, Spec: api.IPv6DelegatedAddressSpec{Interface: "lan", PrefixDelegation: "wan-pd", SubnetID: "1", AddressSuffix: "::1"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv6RouterAdvertisement"}, Metadata: api.ObjectMeta{Name: "lan-ra"}, Spec: api.IPv6RouterAdvertisementSpec{Interface: "lan", PrefixFrom: api.StatusValueSourceSpec{Resource: "IPv6DelegatedAddress/lan-v6", Field: "address"}, RouterLifetime: "60"}},
	}}}
	store, err := state.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lease, _, err := store.ConfigurePDLease("wan-pd", state.PDLeaseConfig{Client: api.IPv6PDClientRouterd, UplinkIfname: "wan0", ClientDUID: "01", IAID: 1})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	lease.CurrentPrefix = "2001:db8:1200::/60"
	lease.LastReplyAt, lease.SnapshotUpdatedAt, lease.LeaseUpdatedAt = now, now, now
	lease.PreferredSeconds, lease.ValidSeconds = 90, 120
	lease.Phase = daemonapi.ResourcePhaseBound
	if err := store.SavePDLease("wan-pd", lease); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "Interface", "lan", map[string]any{"phase": "Up", "ifname": "vrf-ra-lan"}); err != nil {
		t.Fatal(err)
	}
	parent := VRFController{Router: router, Store: store}
	if err := parent.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	run("ip", "-6", "address", "add", "2001:db8:999::1/64", "dev", "vrf-ra-wan")
	lan := LANAddressController{Router: router, DeclaredRouter: router, Store: store, VRFs: &parent}
	reconcileLAN := func() {
		deadline := time.Now().Add(10 * time.Second)
		for {
			if err := lan.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			status, err := store.ReadObjectStatus(api.NetAPIVersion, "IPv6DelegatedAddress", "lan-v6")
			if err != nil {
				t.Fatal(err)
			}
			if status["phase"] == "Applied" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("delegated address did not become usable: %+v", status)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	reconcileLAN()
	service := NewRouterAdvertisementService(ctx, false)
	service.vrfs = &parent
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := service.Stop(stop); err != nil {
			t.Error(err)
		}
	}()
	if err := service.Reconcile(ctx, router, router, store); err != nil {
		t.Fatalf("RA: %v; addresses: %s; routes: %s", err, run("ip", "-6", "address", "show"), run("ip", "-6", "route", "show", "table", "all"))
	}
	checkClient := func(prefix string) {
		deadline := time.Now().Add(10 * time.Second)
		for {
			if err := service.Reconcile(ctx, router, router, store); err != nil {
				t.Fatal(err)
			}
			addresses := client("ip", "-6", "-o", "address", "show", "dev", "vrf-ra-client", "scope", "global")
			routes := client("ip", "-6", "route", "show", "default")
			if strings.Contains(addresses, prefix) && !strings.Contains(addresses, "tentative") && strings.Contains(routes, "proto ra") {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("SLAAC/default missing: %s %s", addresses, routes)
			}
			time.Sleep(100 * time.Millisecond)
		}
		route := client("ip", "-6", "route", "get", "2001:db8:999::2")
		if !strings.Contains(route, "src "+prefix) {
			t.Fatalf("transit uses wrong source: %s", route)
		}
		client("ping", "-6", "-c", "2", "-W", "2", prefix+":1")
		client("ping", "-6", "-c", "2", "-W", "2", "2001:db8:999::2")
	}
	checkClient("2001:db8:1200:1:")
	now = time.Now().UTC()
	lease.PreviousPrefixes = []state.PDPreviousPrefix{{Prefix: lease.CurrentPrefix, ExpiresAt: now.Add(60 * time.Second)}}
	lease.CurrentPrefix = "2001:db8:1300::/60"
	lease.LastReplyAt, lease.SnapshotUpdatedAt, lease.LeaseUpdatedAt = now, now, now
	if err := store.SavePDLease("wan-pd", lease); err != nil {
		t.Fatal(err)
	}
	reconcileLAN()
	if err := service.Reconcile(ctx, router, router, store); err != nil {
		t.Fatalf("RA: %v; addresses: %s; routes: %s", err, run("ip", "-6", "address", "show"), run("ip", "-6", "route", "show", "table", "all"))
	}
	checkClient("2001:db8:1300:1:")
	old := client("ip", "-6", "-o", "address", "show", "dev", "vrf-ra-client", "to", "2001:db8:1200:1::/64")
	if !strings.Contains(old, "deprecated") || !strings.Contains(old, "preferred_lft 0sec") {
		t.Fatalf("previous prefix not deprecated: %s", old)
	}
	empty := &api.Router{}
	if err := service.Reconcile(ctx, empty, empty, store); err != nil {
		t.Fatal(err)
	}
	lan.Router, lan.DeclaredRouter = empty, empty
	if err := lan.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	parent.Router = empty
	if err := parent.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if addresses := run("ip", "-6", "address", "show", "dev", "vrf-ra-lan"); strings.Contains(addresses, "2001:db8:1200:") || strings.Contains(addresses, "2001:db8:1300:") {
		t.Fatalf("owned address retained: %s", addresses)
	}
	if routes := client("ip", "-6", "route", "show", "default"); routes != "" {
		t.Fatalf("withdrawn RA default retained: %s", routes)
	}
	if journal, err := readRAJournal(store); err != nil || len(journal) != 0 {
		t.Fatalf("RA receipt retained: %+v %v", journal, err)
	}
	t.Log("finite delegated address, real RA/SLAAC, IPv6 transit, renumbering and withdrawal passed inside VRF")
}
