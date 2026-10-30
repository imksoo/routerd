// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package chain

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func tunnelNativeReviewFixture(t *testing.T, fromSource, acquire bool) (TunnelInterfaceController, *routerstate.SQLiteStore, func(...string) []byte) {
	t.Helper()
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires ROUTERD_RUN_NETNS=1 in an isolated network namespace")
	}
	currentNS, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	hostNS := os.Getenv("ROUTERD_HOST_NETNS")
	if !strings.HasPrefix(hostNS, "net:[") || currentNS == hostNS {
		t.Fatal("outer network namespace isolation is unverified")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	run := func(args ...string) []byte {
		t.Helper()
		out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("ip %v: %v: %s", args, err, out)
		}
		return out
	}
	run("link", "add", "name", "tn-review-under", "type", "dummy")
	cleanupLink := func(name string) {
		cleanupCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_, _ = exec.CommandContext(cleanupCtx, "ip", "link", "delete", "dev", name).CombinedOutput()
	}
	t.Cleanup(func() { cleanupLink("tn-review-under") })
	run("address", "add", "198.18.10.1/30", "dev", "tn-review-under")
	run("link", "set", "dev", "tn-review-under", "up")
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	spec := api.TunnelInterfaceSpec{Mode: "ipip", Local: "198.18.10.1", Remote: "198.18.10.2", Address: "192.0.2.1/32", MTU: 1400, TrustedUnderlay: true}
	if fromSource {
		spec.Local = ""
		spec.LocalFrom = api.StatusValueSourceSpec{Resource: "DHCPv4Client/underlay", Field: "currentAddress"}
		if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "underlay", map[string]any{"phase": "Bound", "currentAddress": "198.18.10.1/30"}); err != nil {
			t.Fatal(err)
		}
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tn-review"}, Spec: spec,
	}}}}
	parent := TunnelInterfaceController{Router: router, Store: eventedStore{Store: store, Router: router}}
	t.Cleanup(func() { cleanupLink("tn-review") })
	if acquire {
		if err := parent.Reconcile(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return parent, store, run
}

func TestTunnelNativeReviewKeepsReplacementAfterAddressObservation(t *testing.T) {
	parent, _, run := tunnelNativeReviewFixture(t, false, true)
	original, err := net.InterfaceByName("tn-review")
	if err != nil {
		t.Fatal(err)
	}
	spec := parent.Router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
	spec.Address = "192.0.2.2/32"
	parent.Router.Spec.Resources[0].Spec = spec
	replaced, replacementIndex := false, 0
	parent.ObserveNative = func(ctx context.Context, name string) (tunnelNativeLink, error) {
		link, err := observeTunnelNativeLink(ctx, name)
		if err == nil && link.Exists && !replaced {
			replaced = true
			run("link", "delete", "dev", "tn-review")
			run("link", "add", "name", "tn-review", "type", "ipip", "local", "198.18.10.1", "remote", "198.18.10.2")
			run("address", "add", "192.0.2.1/32", "dev", "tn-review")
			foreign, lookupErr := net.InterfaceByName("tn-review")
			if lookupErr != nil {
				t.Fatal(lookupErr)
			}
			replacementIndex = foreign.Index
			if replacementIndex == original.Index {
				t.Fatal("independent replacement did not change the native link index")
			}
		}
		return link, err
	}
	reconcileErr := parent.Reconcile(t.Context())
	if !replaced {
		t.Fatal("the actual final IPv4 observation boundary was not exercised")
	}
	foreign, lookupErr := net.InterfaceByName("tn-review")
	if lookupErr != nil || foreign.Index != replacementIndex {
		t.Fatalf("foreign native identity was lost: old=%d replacement=%d current=%+v reconcile=%v lookup=%v", original.Index, replacementIndex, foreign, reconcileErr, lookupErr)
	}
	addresses := run("-j", "-4", "address", "show", "dev", "tn-review")
	if !strings.Contains(string(addresses), "192.0.2.1") || strings.Contains(string(addresses), "192.0.2.2") {
		t.Fatalf("late native replacement was mutated after cached address observation: old=%d foreign=%d reconcile=%v native=%s", original.Index, replacementIndex, reconcileErr, addresses)
	}
	t.Logf("independent replacement retained: old index=%d foreign index=%d reconcile=%v", original.Index, replacementIndex, reconcileErr)
}

func TestTunnelNativeReviewDoesNotOverwriteReplacementReceipt(t *testing.T) {
	parent, _, run := tunnelNativeReviewFixture(t, false, true)
	spec := parent.Router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
	spec.Address = "192.0.2.2/32"
	parent.Router.Spec.Resources[0].Spec = spec
	replaced := false
	var replacement tunnelNativeReceipt
	parent.ObserveNative = func(ctx context.Context, name string) (tunnelNativeLink, error) {
		link, err := observeTunnelNativeLink(ctx, name)
		if err == nil && link.Exists && !replaced {
			replaced = true
			records, readErr := parent.tunnelNativeReceipts()
			if readErr != nil || len(records) != 1 {
				t.Fatalf("read actual receipt before replacement: %+v %v", records, readErr)
			}
			previous := records[0]
			replacement = previous
			replacement.Token = strings.Repeat("1", 32)
			if replacement.Token == previous.Token {
				replacement.Token = strings.Repeat("2", 32)
			}
			run("link", "set", "dev", "tn-review", "alias", "routerd-tunnel:"+replacement.Token)
			if err := parent.saveTunnelNative(&previous, replacement, false); err != nil {
				t.Fatal(err)
			}
		}
		return link, err
	}
	reconcileErr := parent.Reconcile(t.Context())
	if !replaced {
		t.Fatal("receipt replacement did not cross an actual native observation")
	}
	records, err := parent.tunnelNativeReceipts()
	if err != nil || len(records) != 1 || records[0] != replacement {
		t.Fatalf("new receipt was overwritten or removed: actual=%+v expected=%+v reconcile=%v read=%v", records, replacement, reconcileErr, err)
	}
	link, err := observeTunnelNativeLink(t.Context(), "tn-review")
	if err != nil || !link.Exists || link.Index != replacement.Index || link.Token != replacement.Token {
		t.Fatalf("new native ownership was changed: %+v reconcile=%v read=%v", link, reconcileErr, err)
	}
	addresses := run("-j", "-4", "address", "show", "dev", "tn-review")
	if !strings.Contains(string(addresses), "192.0.2.1") || strings.Contains(string(addresses), "192.0.2.2") {
		t.Fatalf("old intent changed the new owner's addresses: %s reconcile=%v", addresses, reconcileErr)
	}
}

func TestTunnelNativeReviewWithdrawsSourceExpiredDuringObservation(t *testing.T) {
	parent, store, _ := tunnelNativeReviewFixture(t, true, true)
	spec := parent.Router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
	spec.Address = "192.0.2.2/32"
	parent.Router.Spec.Resources[0].Spec = spec
	expired := false
	parent.ObserveNative = func(ctx context.Context, name string) (tunnelNativeLink, error) {
		link, err := observeTunnelNativeLink(ctx, name)
		if err == nil && link.Exists && !expired {
			expired = true
			if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "underlay", map[string]any{"phase": "Expired", "currentAddress": ""}); err != nil {
				t.Fatal(err)
			}
		}
		return link, err
	}
	reconcileErr := parent.Reconcile(t.Context())
	if !expired {
		t.Fatal("source expiry did not cross an actual native observation")
	}
	link, err := observeTunnelNativeLink(t.Context(), "tn-review")
	if err != nil || link.Exists {
		t.Fatalf("expired source retained an applied native tunnel: %+v reconcile=%v read=%v", link, reconcileErr, err)
	}
	status, err := store.ReadObjectStatus(api.HybridAPIVersion, "TunnelInterface", "tn-review")
	if err != nil || status["phase"] == "Up" {
		t.Fatalf("expired source was published as Up: %+v reconcile=%v read=%v", status, reconcileErr, err)
	}
}

func TestTunnelNativeReviewRefusesChildBeforeSourceWithdrawal(t *testing.T) {
	parent, store, run := tunnelNativeReviewFixture(t, true, true)
	original, err := net.InterfaceByName("tn-review")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "underlay", map[string]any{"phase": "Expired", "currentAddress": ""}); err != nil {
		t.Fatal(err)
	}
	parent.Router.Spec.Resources = append(parent.Router.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "late-child"},
		Spec: api.IPv4StaticAddressSpec{Interface: "tn-review", Address: "192.0.2.99/32"},
	})
	child := IPv4StaticAddressController{Router: parent.Router, Store: store}
	childErr := child.Reconcile(t.Context())
	present, err := net.InterfaceByName("tn-review")
	if err != nil || present.Index != original.Index {
		t.Fatalf("read-only child parent check changed native ownership: original=%+v current=%+v child=%v lookup=%v", original, present, childErr, err)
	}
	if addresses := run("-j", "-4", "address", "show", "dev", "tn-review"); strings.Contains(string(addresses), "192.0.2.99") {
		t.Fatalf("child acquired parent whose source expired before parent withdrawal: child=%v native=%s", childErr, addresses)
	}
}

func TestTunnelNativeReviewWithdrawsSourceExpiredAfterCreateACK(t *testing.T) {
	parent, store, _ := tunnelNativeReviewFixture(t, true, false)
	created := false
	parent.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err == nil && name == "ip" && strings.HasPrefix(strings.Join(args, " "), "link add dev tn-review ") {
			created = true
			if err := store.SaveObjectStatus(api.NetAPIVersion, "DHCPv4Client", "underlay", map[string]any{"phase": "Expired", "currentAddress": ""}); err != nil {
				t.Fatal(err)
			}
		}
		return out, err
	}
	reconcileErr := parent.Reconcile(t.Context())
	if !created {
		t.Fatalf("actual exclusive creation ACK boundary was not exercised: %v", reconcileErr)
	}
	link, err := observeTunnelNativeLink(t.Context(), "tn-review")
	if err != nil || link.Exists {
		t.Fatalf("known creation ACK plus expired source retained a bare native tunnel: %+v reconcile=%v read=%v", link, reconcileErr, err)
	}
}
