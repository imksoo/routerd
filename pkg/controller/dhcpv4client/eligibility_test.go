// SPDX-License-Identifier: BSD-3-Clause

package dhcpv4client

import (
	"context"
	"errors"
	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"strings"
	"testing"
	"time"
)

type gatedLeaseStore struct {
	mapStore
	variables *routerstate.MemoryStore
	readErr   error
}

func (s *gatedLeaseStore) Get(name string) routerstate.Value { return s.variables.Get(name) }
func (s *gatedLeaseStore) ReadVariable(name string) (routerstate.Value, error) {
	if s.readErr != nil {
		return routerstate.Value{}, s.readErr
	}
	return s.variables.ReadVariable(name)
}
func (s *gatedLeaseStore) Age(name string) time.Duration { return s.variables.Age(name) }
func (s *gatedLeaseStore) Now() time.Time                { return s.variables.Now() }

func TestWhenFalseWithdrawsValidLeaseWithoutPollingWorker(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	var removed bool
	c := effectController(func(_ context.Context, name string, args ...string) ([]byte, error) {
		line := name + " " + strings.Join(args, " ")
		switch line {
		case "ip -4 addr show dev eth0":
			return []byte("inet 192.0.2.10/24 scope global eth0\n"), nil
		case "ip -4 addr del 192.0.2.10/24 dev eth0":
			removed = true
			return nil, nil
		}
		return nil, errors.New("unexpected command: " + line)
	})
	c.Router.Spec.Resources[0].Spec = api.DHCPv4ClientSpec{Interface: "eth0", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"role": {Equals: "MASTER"}}}}
	key := api.NetAPIVersion + "/DHCPv4Client/wan"
	variables := routerstate.NewMemory()
	if _, err := variables.Set("role", "BACKUP", "test"); err != nil {
		t.Fatal(err)
	}
	store := &gatedLeaseStore{mapStore: mapStore{key: {"phase": "Bound", "ifname": "eth0", "appliedAddress": "192.0.2.10/24", "expiresAt": time.Now().Add(time.Hour).Format(time.RFC3339Nano)}}, variables: variables}
	c.Store = store
	if err := c.Reconcile(context.Background(), "wan"); err != nil {
		t.Fatal(err)
	}
	if !removed || mapString(store.mapStore[key], "reason") != "WhenFalse" || mapString(store.mapStore[key], "appliedAddress") != "" {
		t.Fatalf("inactive intent retained a valid lease effect: %#v", store.mapStore[key])
	}
}

func TestWhenReadFailurePreservesAuthorityAndReportsError(t *testing.T) {
	c := effectController(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("unknown eligibility caused host effect")
		return nil, nil
	})
	c.Router.Spec.Resources[0].Spec = api.DHCPv4ClientSpec{Interface: "eth0", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"role": {Equals: "MASTER"}}}}
	key := api.NetAPIVersion + "/DHCPv4Client/wan"
	store := &gatedLeaseStore{mapStore: mapStore{key: {"ifname": "eth0", "appliedAddress": "192.0.2.10/24"}}, variables: routerstate.NewMemory(), readErr: errors.New("state storage failed")}
	c.Store = store
	if err := c.Reconcile(context.Background(), "wan"); err == nil {
		t.Fatal("failed state read was hidden")
	}
	if mapString(store.mapStore[key], "appliedAddress") == "" {
		t.Fatal("failed state read discarded effect ownership")
	}
}

func TestWhenUnknownDoesNotApplyOrAcquireAuthority(t *testing.T) {
	c := effectController(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("unresolved role caused host effect")
		return nil, nil
	})
	c.Router.Spec.Resources[0].Spec = api.DHCPv4ClientSpec{Interface: "eth0", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"role": {Equals: "MASTER"}}}}
	variables := routerstate.NewMemory()
	if _, err := variables.Set("role", "unknown", "test"); err != nil {
		t.Fatal(err)
	}
	store := &gatedLeaseStore{mapStore: mapStore{}, variables: variables}
	c.Store = store
	if err := c.Reconcile(context.Background(), "wan"); err != nil {
		t.Fatal(err)
	}
	if status := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", "wan"); mapString(status, "reason") != "WhenFalse" || mapString(status, "currentAddress") != "" {
		t.Fatalf("unknown role reported authority: %#v", status)
	}
}

type failingOwnershipReadStore struct {
	*gatedLeaseStore
	err error
}

func (s *failingOwnershipReadStore) ReadObjectStatus(string, string, string) (map[string]any, error) {
	return nil, s.err
}

func TestWhenInactiveOwnershipReadFailureStopsEffects(t *testing.T) {
	c := effectController(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("failed ownership read caused a host effect")
		return nil, nil
	})
	c.Router.Spec.Resources[0].Spec = api.DHCPv4ClientSpec{Interface: "eth0", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"role": {Equals: "MASTER"}}}}
	variables := routerstate.NewMemory()
	if _, err := variables.Set("role", "BACKUP", "test"); err != nil {
		t.Fatal(err)
	}
	key := api.NetAPIVersion + "/DHCPv4Client/wan"
	failure := errors.New("ownership row unreadable")
	store := &failingOwnershipReadStore{gatedLeaseStore: &gatedLeaseStore{mapStore: mapStore{key: {"ifname": "eth0", "appliedAddress": "192.0.2.10/24"}}, variables: variables}, err: failure}
	c.Store = store
	if err := c.Reconcile(context.Background(), "wan"); !errors.Is(err, failure) {
		t.Fatalf("failed read was hidden: %v", err)
	}
	if mapString(store.mapStore[key], "appliedAddress") == "" {
		t.Fatal("failed read lost owned effects")
	}
}

type failingOwnershipListStore struct {
	*gatedLeaseStore
	reads int
}

func (s *failingOwnershipListStore) ListObjectStatuses() ([]routerstate.ObjectStatus, error) {
	return nil, errors.New("ownership enumeration failed")
}
func (s *failingOwnershipListStore) ReadObjectStatus(v, k, n string) (map[string]any, error) {
	s.reads++
	return s.gatedLeaseStore.ReadObjectStatus(v, k, n)
}

func TestWhenEnumerationFailureStopsLiveLeaseReconcile(t *testing.T) {
	c := effectController(func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("failed enumeration caused a host effect")
		return nil, nil
	})
	c.Router.Spec.Resources[0].Spec = api.DHCPv4ClientSpec{Interface: "eth0", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"role": {Equals: "MASTER"}}}}
	variables := routerstate.NewMemory()
	if _, err := variables.Set("role", "BACKUP", "test"); err != nil {
		t.Fatal(err)
	}
	store := &failingOwnershipListStore{gatedLeaseStore: &gatedLeaseStore{mapStore: mapStore{}, variables: variables}}
	c.Store = store
	if err := c.ReconcileAll(context.Background()); err == nil {
		t.Fatal("failed enumeration fabricated empty ownership")
	}
	if store.reads != 0 {
		t.Fatal("continued live lease reconciliation after an ownership enumeration failure")
	}
}
