// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestDryRunUnavailableWorkerPreservesLiveAndPendingEffectsForCleanup(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	addressPresent, routePresent := false, false
	var commands []string
	c := effectController(func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		commands = append(commands, line)
		switch line {
		case "ip -4 addr show dev eth0":
			if addressPresent {
				return []byte("inet 192.0.2.10/24 scope global eth0\n"), nil
			}
		case "ip -4 route show default":
			if routePresent {
				return []byte("default via 192.0.2.1 dev eth0 metric 100\n"), nil
			}
		case "ip -4 addr add 192.0.2.10/24 dev eth0":
			addressPresent = true
		case "ip -4 route add default via 192.0.2.1 dev eth0 metric 100":
			routePresent = true
			return nil, errors.New("route result lost after native effect")
		case "ip -4 route del default via 192.0.2.1 dev eth0 metric 100":
			routePresent = false
		case "ip -4 addr del 192.0.2.10/24 dev eth0":
			addressPresent = false
		default:
			t.Fatalf("unexpected native command: %s", line)
		}
		return nil, nil
	})
	expiry := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	status := daemonapi.DaemonStatus{Resources: []daemonapi.ResourceStatus{{
		Resource: daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv4Client", Name: "wan"}, Phase: daemonapi.ResourcePhaseBound,
		Observed: map[string]string{"interface": "eth0", "currentAddress": "192.0.2.10", "prefixLength": "24", "defaultGateway": "192.0.2.1", "expiresAt": expiry},
	}}}
	if err := c.applyDaemonStatus(t.Context(), "wan", status); err == nil {
		t.Fatal("uncertain route application was reported successful")
	}
	before, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	if err != nil || mapString(before, "appliedAddress") == "" || mapString(before, "pendingGateway") == "" {
		t.Fatalf("test did not establish live and pending ownership: %+v %v", before, err)
	}
	c.DryRun = true
	c.DaemonSockets = map[string]string{"wan": filepath.Join(t.TempDir(), "unavailable.sock")}
	commands = nil
	if err := c.Reconcile(t.Context(), "wan"); err != nil {
		t.Fatal(err)
	}
	after, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ifname", "appliedAddress", "pendingGateway", "pendingRouteInterface", "pendingRouteMetric", "expiresAt"} {
		if mapString(after, field) != mapString(before, field) {
			t.Fatalf("dry-run unavailable worker lost %s: before=%+v after=%+v", field, before, after)
		}
	}
	if after["phase"] != daemonapi.ResourcePhasePending || after["reason"] != "DaemonUnavailable" || after["dryRun"] != true || len(commands) != 0 {
		t.Fatalf("dry-run did not report pending without effects: %+v commands=%v", after, commands)
	}
	c.DryRun = false
	if err := c.ReconcileAllForRouter(t.Context(), &api.Router{}, c.Store); err != nil {
		t.Fatal(err)
	}
	remaining, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan")
	if err != nil || len(remaining) != 0 || addressPresent || routePresent {
		t.Fatalf("resumed cleanup left native effects or recovery row: %+v %v", remaining, err)
	}
	want := "ip -4 route show default\nip -4 route show default\nip -4 route del default via 192.0.2.1 dev eth0 metric 100\nip -4 addr show dev eth0\nip -4 addr del 192.0.2.10/24 dev eth0"
	if strings.Join(commands, "\n") != want {
		t.Fatalf("cleanup did not recover and withdraw exact retained effects: %v", commands)
	}
}

type pendingDaemonReadFaultStore struct {
	mapStore
	reads, writes int
	err           error
}

func (s *pendingDaemonReadFaultStore) ReadObjectStatus(version, kind, name string) (map[string]any, error) {
	s.reads++
	if s.reads == 2 {
		return nil, s.err
	}
	return s.mapStore.ReadObjectStatus(version, kind, name)
}

func (s *pendingDaemonReadFaultStore) SaveObjectStatus(version, kind, name string, status map[string]any) error {
	s.writes++
	return s.mapStore.SaveObjectStatus(version, kind, name, status)
}

func TestDryRunUnavailableWorkerOwnershipReadFailurePreventsStatusOverwrite(t *testing.T) {
	key := api.NetAPIVersion + "/DHCPv4Client/wan"
	failure := errors.New("ownership row unavailable before pending publication")
	store := &pendingDaemonReadFaultStore{mapStore: mapStore{key: {"ifname": "eth0", "appliedAddress": "192.0.2.10/24"}}, err: failure}
	c := effectController(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("dry-run read failure performed native effects")
		return nil, nil
	})
	c.DryRun, c.Store = true, store
	c.DaemonSockets = map[string]string{"wan": filepath.Join(t.TempDir(), "unavailable.sock")}
	if err := c.Reconcile(t.Context(), "wan"); !errors.Is(err, failure) {
		t.Fatalf("ownership read failure was hidden: %v", err)
	}
	if store.reads != 2 || store.writes != 0 || mapString(store.mapStore[key], "appliedAddress") != "192.0.2.10/24" || len(store.mapStore[key]) != 2 {
		t.Fatalf("failed boundary read changed recovery status: reads=%d writes=%d status=%+v", store.reads, store.writes, store.mapStore[key])
	}
}
