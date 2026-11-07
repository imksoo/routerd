// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestRemovedLeaseCleanupRetainsPartialFailureAndRetries(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	addressPresent, routePresent, failRoute := true, true, true
	var calls []string
	c := effectController(func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		calls = append(calls, line)
		switch line {
		case "ip -4 addr show dev eth0":
			if addressPresent {
				return []byte("inet 192.0.2.10/24 scope global eth0\ninet 192.0.2.200/24 scope global eth0\n"), nil
			}
			return nil, nil
		case "ip -4 route show default":
			if routePresent {
				return []byte("default via 192.0.2.1 dev eth0 metric 100\ndefault via 198.51.100.1 dev other metric 200\n"), nil
			}
			return nil, nil
		case "ip -4 route del default via 192.0.2.1 dev eth0 metric 100":
			if failRoute {
				return nil, errors.New("route busy")
			}
			routePresent = false
			return nil, nil
		case "ip -4 addr del 192.0.2.10/24 dev eth0":
			addressPresent = false
			return nil, nil
		}
		return nil, errors.New("unexpected command: " + line)
	})
	c.Router = &api.Router{}
	key := api.NetAPIVersion + "/DHCPv4Client/removed"
	foreignKey := api.NetAPIVersion + "/DHCPv4Client/foreign"
	store := mapStore{key: {"managedBy": "routerd", "ifname": "eth0", "appliedAddress": "192.0.2.10/24", "appliedDefaultGateway": "192.0.2.1", "appliedRouteMetric": 100}, foreignKey: {"managedBy": "external", "ifname": "other", "appliedAddress": "198.51.100.10/24"}}
	c.Store = store
	if err := c.ReconcileAll(context.Background()); err == nil {
		t.Fatal("failed route cleanup reported success")
	}
	if addressPresent || !routePresent || store[key] == nil || mapString(store[key], "appliedAddress") != "" || mapString(store[key], "appliedDefaultGateway") != "192.0.2.1" {
		t.Fatalf("partial cleanup forgot ownership: %#v", store[key])
	}
	failRoute = false
	calls = nil
	if err := c.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store[key] != nil || routePresent || store[foreignKey] == nil {
		t.Fatalf("removed owner not cleaned or foreign owner deleted: %#v", store)
	}
	if strings.Join(calls, "\n") != "ip -4 route show default\nip -4 route del default via 192.0.2.1 dev eth0 metric 100" {
		t.Fatalf("retry did not resume pending exact route: %v", calls)
	}
}

func TestRemovedLeaseRecoversPendingEffectsBeforeDeletion(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	path := filepath.Join(t.TempDir(), "resolv.conf")
	if err := writeResolvConf(path, "removed", []string{"192.0.2.53"}, false); err != nil {
		t.Fatal(err)
	}
	present := true
	c := effectController(func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		switch line {
		case "ip -4 addr show dev eth0":
			if present {
				return []byte("inet 192.0.2.10/24 scope global eth0\n"), nil
			}
			return nil, nil
		case "ip -4 addr del 192.0.2.10/24 dev eth0":
			present = false
			return nil, nil
		}
		return nil, errors.New("unexpected command: " + line)
	})
	c.Router = &api.Router{}
	c.ResolvConfPath = path
	key := api.NetAPIVersion + "/DHCPv4Client/removed"
	store := mapStore{key: {"managedBy": "routerd", "pendingAddress": "192.0.2.10/24", "pendingAddressInterface": "eth0", "pendingDNSServers": "192.0.2.53", "pendingDNSInterface": "eth0"}}
	c.Store = store
	if err := c.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if present || store[key] != nil || strings.Contains(string(data), "nameserver") {
		t.Fatalf("deleted status before pending effects were withdrawn: %#v %s", store, data)
	}
}

func TestRemovedLeaseDryRunAndEnumerationFailureKeepOwnership(t *testing.T) {
	c := effectController(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("unexpected host effect")
		return nil, nil
	})
	c.Router = &api.Router{}
	c.DryRun = true
	key := api.NetAPIVersion + "/DHCPv4Client/removed"
	store := mapStore{key: {"managedBy": "routerd", "ifname": "eth0", "appliedAddress": "192.0.2.10/24"}}
	c.Store = store
	if err := c.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store[key] == nil || mapString(store[key], "appliedAddress") == "" {
		t.Fatal("dry-run deleted recovery ownership")
	}
	c.DryRun = false
	c.Store = &failedEffectStore{saved: store, err: errors.New("enumeration unavailable")}
	if err := c.ReconcileAll(context.Background()); err == nil {
		t.Fatal("failed enumeration fabricated absence")
	}
	if store[key] == nil {
		t.Fatal("failed enumeration deleted ownership")
	}
}

type deleteFailureStore struct {
	mapStore
	fail bool
}

func (s *deleteFailureStore) DeleteObject(v, k, n string) error {
	if s.fail {
		return errors.New("delete failed")
	}
	return s.mapStore.DeleteObject(v, k, n)
}

func TestRemovedLeaseStatusDeleteFailureKeepsWithdrawnRecord(t *testing.T) {
	c := effectController(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("unexpected host effect")
		return nil, nil
	})
	c.Router = &api.Router{}
	key := api.NetAPIVersion + "/DHCPv4Client/removed"
	store := &deleteFailureStore{mapStore: mapStore{key: {"managedBy": "routerd"}}, fail: true}
	c.Store = store
	if err := c.ReconcileAll(context.Background()); err == nil {
		t.Fatal("failed delete was hidden")
	}
	if store.mapStore[key] == nil || mapString(store.mapStore[key], "phase") != "Released" {
		t.Fatal("failed deletion discarded record")
	}
	store.fail = false
	if err := c.ReconcileAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.mapStore[key] != nil {
		t.Fatal("did not retry ownership record deletion")
	}
}
