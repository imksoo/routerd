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

func TestTunnelNativeReviewKeepsReplacementAfterAddressObservation(t *testing.T) {
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
	defer cancel()
	run := func(args ...string) []byte {
		t.Helper()
		out, err := exec.CommandContext(ctx, "ip", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("ip %v: %v: %s", args, err, out)
		}
		return out
	}
	run("link", "add", "name", "tn-review-under", "type", "dummy")
	defer func() {
		_, _ = exec.CommandContext(ctx, "ip", "link", "delete", "dev", "tn-review-under").CombinedOutput()
	}()
	run("address", "add", "198.18.10.1/30", "dev", "tn-review-under")
	run("link", "set", "dev", "tn-review-under", "up")
	store, err := routerstate.OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "tn-review"},
		Spec:     api.TunnelInterfaceSpec{Mode: "ipip", Local: "198.18.10.1", Remote: "198.18.10.2", Address: "192.0.2.1/32", MTU: 1400, TrustedUnderlay: true},
	}}}}
	parent := TunnelInterfaceController{Router: router, Store: eventedStore{Store: store, Router: router}}
	if err := parent.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = exec.CommandContext(ctx, "ip", "link", "delete", "dev", "tn-review").CombinedOutput() }()
	original, err := net.InterfaceByName("tn-review")
	if err != nil {
		t.Fatal(err)
	}
	spec := router.Spec.Resources[0].Spec.(api.TunnelInterfaceSpec)
	spec.Address = "192.0.2.2/32"
	router.Spec.Resources[0].Spec = spec
	replaced, replacementIndex := false, 0
	parent.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if !replaced && name == "ip" && strings.Join(args, " ") == "-o -4 addr show dev tn-review" {
			if err != nil {
				t.Fatalf("capture actual owned IPv4 addresses: %v: %s", err, out)
			}
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
		return out, err
	}
	reconcileErr := parent.Reconcile(ctx)
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
