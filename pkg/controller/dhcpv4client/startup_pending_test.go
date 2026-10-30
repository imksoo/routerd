// SPDX-License-Identifier: BSD-3-Clause
package dhcpv4client

import (
	"context"
	"encoding/json"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestLiveFirstLeaseWaitsForSupervisedDaemonWithoutFailingApply(t *testing.T) {
	c := effectController(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("waiting for first daemon performed a native effect")
		return nil, nil
	})
	c.DaemonSockets = map[string]string{"wan": filepath.Join(t.TempDir(), "starting.sock")}
	for i := 0; i < 2; i++ {
		if err := c.ReconcileAll(t.Context()); err != nil {
			t.Fatalf("first-start socket absence failed apply: %v", err)
		}
		s, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
		if err != nil || s["phase"] != daemonapi.ResourcePhasePending || s["reason"] != "DaemonUnavailable" || s["dryRun"] != false || mapString(s, "error") == "" {
			t.Fatalf("missing daemon was not honestly pending: %+v %v", s, err)
		}
	}
	listener, err := net.Listen("unix", c.DaemonSockets["wan"])
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(daemonapi.DaemonStatus{Resources: []daemonapi.ResourceStatus{{
			Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: "wan"},
			Phase:    daemonapi.ResourcePhasePending, Observed: map[string]string{"interface": "eth0"},
		}}})
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	if err := c.ReconcileAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	s, _ := c.Store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	if mapString(s, "reason") == "DaemonUnavailable" || mapString(s, "error") != "" || s["interface"] != "eth0" {
		t.Fatalf("available worker did not replace initial dependency wait: %+v", s)
	}
}

func TestMissingDaemonWithLeaseAuthorityStillFailsAndPreservesOwnership(t *testing.T) {
	for _, current := range []map[string]any{
		{"phase": daemonapi.ResourcePhaseBound, "currentAddress": "192.0.2.10", "expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)},
		{"ifname": "eth0", "appliedAddress": "192.0.2.10/24"},
		{"pendingGateway": "192.0.2.1", "pendingRouteInterface": "eth0"},
		{"appliedDNSServers": "192.0.2.53", "appliedDNSInterface": "eth0"},
	} {
		before := maps.Clone(current)
		c := effectController(func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("unavailable unexpired daemon performed native effects")
			return nil, nil
		})
		c.DaemonSockets = map[string]string{"wan": filepath.Join(t.TempDir(), "gone.sock")}
		key := api.NetAPIVersion + "/DHCPv4Client/wan"
		c.Store = mapStore{key: current}
		if err := c.ReconcileAll(t.Context()); err == nil {
			t.Fatalf("lost existing worker was treated as first startup: %+v", current)
		}
		after, _ := c.Store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("unavailable worker lost ownership: before=%+v after=%+v", before, after)
		}
	}
}

func TestFirstDaemonNonSocketFailureIsNotStartupPending(t *testing.T) {
	c := effectController(nil)
	path := filepath.Join(t.TempDir(), "not-a-socket")
	if err := os.WriteFile(path, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	c.DaemonSockets = map[string]string{"wan": path}
	if err := c.ReconcileAll(t.Context()); err == nil {
		t.Fatal("foreign file at daemon endpoint was accepted as startup")
	}
}
