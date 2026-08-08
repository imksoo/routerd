// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/vxlan"
)

func TestBridgeControllerWritesPersistentArtifactsAndReadsKernelState(t *testing.T) {
	dir := t.TempDir()
	store := mapStore{}
	router := bridgeVXLANTestRouter(false)
	var commands [][]string
	controller := BridgeController{
		Router:          router,
		Store:           store,
		NetworkdDir:     dir,
		OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) error {
			commands = append(commands, append([]string{name}, args...))
			return nil
		},
		Lookup: func(name string) (*net.Interface, error) {
			if name != "br-l2" {
				return nil, os.ErrNotExist
			}
			return &net.Interface{Index: 12, Name: name, MTU: 1370, Flags: net.FlagUp}, nil
		},
		ReadFile: func(path string) ([]byte, error) {
			switch filepath.Base(path) {
			case "stp_state":
				return []byte("1\n"), nil
			case "multicast_snooping":
				return []byte("0\n"), nil
			default:
				return nil, os.ErrNotExist
			}
		},
		Readlink: func(path string) (string, error) {
			if strings.HasSuffix(path, filepath.Join("ens19", "master")) {
				return "../../br-l2", nil
			}
			return "", os.ErrNotExist
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	for _, path := range []string{
		filepath.Join(dir, "30-routerd-br-l2.netdev"),
		filepath.Join(dir, "30-routerd-br-l2.network"),
		filepath.Join(dir, "10-netplan-ens19.network.d", "88-routerd-bridge.conf"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("persistent artifact %s: %v", path, err)
		}
	}
	if phase := store[api.NetAPIVersion+"/Bridge/legacy-l2"]["phase"]; phase != "Healthy" {
		t.Fatalf("phase = %v, status=%#v", phase, store)
	}
	if len(commands) == 0 || !reflect.DeepEqual(commands[0], []string{"networkctl", "reload"}) {
		t.Fatalf("commands = %#v", commands)
	}
	commands = nil
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if len(commands) != 0 {
		t.Fatalf("second reconcile commands = %#v, want none", commands)
	}
}

func TestVXLANTunnelControllerCreatesAndThenConvergesWithoutDuplicateFDB(t *testing.T) {
	store := mapStore{}
	router := bridgeVXLANTestRouter(true)
	exists := false
	fdb := false
	var commands [][]string
	controller := VXLANTunnelController{
		Router:          router,
		DeclaredRouter:  router,
		Store:           store,
		NetworkdDir:     t.TempDir(),
		OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			cmd := append([]string{name}, args...)
			commands = append(commands, cmd)
			joined := strings.Join(cmd, " ")
			switch {
			case strings.HasPrefix(joined, "ip -details link show dev vx-l2"):
				if !exists {
					return nil, errors.New("not found")
				}
				return []byte("8: vx-l2: <UP> mtu 1370 master br-l2\n    vxlan id 200001 local 10.254.200.1 dev wg-l2 dstport 4789 nolearning df inherit"), nil
			case strings.HasPrefix(joined, "ip link add vx-l2"):
				exists = true
			case strings.HasPrefix(joined, "bridge fdb append"):
				fdb = true
			case strings.HasPrefix(joined, "bridge fdb show dev vx-l2"):
				if fdb {
					return []byte("00:00:00:00:00:00 dst 10.254.200.2 self permanent\n"), nil
				}
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if phase := store[api.NetAPIVersion+"/VXLANTunnel/legacy-l2-overlay"]["phase"]; phase != "Healthy" {
		t.Fatalf("phase = %v, status=%#v", phase, store)
	}
	commands = nil
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	for _, cmd := range commands {
		joined := strings.Join(cmd, " ")
		if strings.Contains(joined, " link add ") || strings.Contains(joined, " fdb append ") {
			t.Fatalf("second reconcile repeated create/append: %v", cmd)
		}
	}
}

type vxlanWhenStore struct {
	mapStore
	values map[string]routerstate.Value
	now    time.Time
}

func (s *vxlanWhenStore) Get(name string) routerstate.Value {
	if value, ok := s.values[name]; ok {
		return value
	}
	return routerstate.Value{Status: routerstate.StatusUnknown, Since: s.Now(), UpdatedAt: s.Now()}
}

func (s *vxlanWhenStore) Age(name string) time.Duration {
	value := s.Get(name)
	if value.Since.IsZero() {
		return 0
	}
	return s.Now().Sub(value.Since)
}

func (s *vxlanWhenStore) Now() time.Time {
	if s.now.IsZero() {
		return time.Now().UTC()
	}
	return s.now
}

func TestVXLANTunnelWhenFalseTeardownIsFailClosedAndOrdered(t *testing.T) {
	router := bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}})
	store := &vxlanWhenStore{
		mapStore: mapStore{api.NetAPIVersion + "/VXLANTunnel/legacy-l2-overlay": {"managedBy": "routerd"}},
		values:   map[string]routerstate.Value{"ha.role": {Status: routerstate.StatusSet, Value: "backup"}},
	}
	dir := t.TempDir()
	writeVXLANOwnedArtifacts(t, dir, "vx-l2")
	exists := true
	var commands []string
	stale := true
	controller := VXLANTunnelController{
		Router:          router,
		DeclaredRouter:  router,
		Store:           store,
		NetworkdDir:     dir,
		OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			joined := strings.Join(append([]string{name}, args...), " ")
			commands = append(commands, joined)
			switch {
			case strings.HasPrefix(joined, "ip -details link show dev vx-l2"):
				if exists {
					return []byte("8: vx-l2: <UP> mtu 1370 master br-l2\n vxlan id 200001 local 10.254.200.1 dev wg-l2 dstport 4789 nolearning"), nil
				}
				return nil, errors.New("not found")
			case joined == "bridge fdb show dev vx-l2":
				if stale {
					return []byte("00:00:00:00:00:00 dst 10.254.200.2 self permanent\n00:00:00:00:00:00 dst 10.254.200.99 self permanent\n"), nil
				}
				return []byte("00:00:00:00:00:00 dst 10.254.200.2 self permanent\n"), nil
			case joined == "bridge fdb del 00:00:00:00:00:00 dev vx-l2 dst 10.254.200.99":
				stale = false
			case joined == "ip link delete dev vx-l2":
				exists = false
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VXLANTunnel", "legacy-l2-overlay")
	if status["phase"] != "Disabled" || status["reason"] != "WhenFalse" || status["forwarding"] != false {
		t.Fatalf("status = %#v", status)
	}
	for _, path := range controller.artifactPaths("vx-l2") {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("artifact %s remains: %v", path, err)
		}
	}
	wantOrder := []string{
		"ip link set dev vx-l2 down",
		"bridge fdb del 00:00:00:00:00:00 dev vx-l2 dst 10.254.200.2",
		"bridge fdb del 00:00:00:00:00:00 dev vx-l2 dst 10.254.200.99",
		"ip link set dev vx-l2 nomaster",
		"ip link delete dev vx-l2",
	}
	assertCommandsInOrder(t, commands, wantOrder)
}

func TestVXLANTunnelUnknownRoleTearsDownInsteadOfForwarding(t *testing.T) {
	router := bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}})
	store := &vxlanWhenStore{mapStore: mapStore{api.NetAPIVersion + "/VXLANTunnel/legacy-l2-overlay": {"managedBy": "routerd"}}, values: map[string]routerstate.Value{
		"ha.role": {Status: routerstate.StatusSet, Value: "unknown"},
	}}
	dir := t.TempDir()
	writeVXLANOwnedArtifacts(t, dir, "vx-l2")
	deleted := false
	exists := true
	controller := VXLANTunnelController{
		Router: router, DeclaredRouter: router, Store: store, NetworkdDir: dir, OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			joined := strings.Join(append([]string{name}, args...), " ")
			if strings.HasPrefix(joined, "ip -details link show dev vx-l2") {
				if !exists {
					return nil, errors.New("not found")
				}
				return []byte("8: vx-l2: <UP> mtu 1370 master br-l2\n vxlan id 200001 local 10.254.200.1 dev wg-l2 dstport 4789 nolearning"), nil
			}
			if joined == "ip link delete dev vx-l2" {
				deleted = true
				exists = false
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VXLANTunnel", "legacy-l2-overlay")
	if !deleted || status["phase"] != "Disabled" || status["reason"] != "WhenUnknown" {
		t.Fatalf("deleted=%t status=%#v", deleted, status)
	}
}

func TestVXLANTunnelWhenFalseRefusesForeignStateAndNeverReportsHealthy(t *testing.T) {
	router := bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}})
	store := &vxlanWhenStore{mapStore: mapStore{api.NetAPIVersion + "/VXLANTunnel/legacy-l2-overlay": {"managedBy": "routerd"}}, values: map[string]routerstate.Value{"ha.role": {Status: routerstate.StatusSet, Value: "backup"}}}
	dir := t.TempDir()
	writeVXLANOwnedArtifacts(t, dir, "vx-l2")
	var deleted bool
	controller := VXLANTunnelController{
		Router: router, DeclaredRouter: router, Store: store, NetworkdDir: dir, OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			joined := strings.Join(append([]string{name}, args...), " ")
			if strings.HasPrefix(joined, "ip -details link show dev vx-l2") {
				return []byte("foreign vxlan"), nil
			}
			if joined == "ip link delete dev vx-l2" {
				deleted = true
			}
			return nil, nil
		},
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if err := controller.Reconcile(context.Background()); err != nil {
			t.Fatalf("Reconcile attempt %d: %v", attempt, err)
		}
	}
	status := store.ObjectStatus(api.NetAPIVersion, "VXLANTunnel", "legacy-l2-overlay")
	if deleted || status["phase"] != "Blocked" || status["reason"] != "ForeignStateWhileGated" || status["managedBy"] != "external" {
		t.Fatalf("deleted=%t status=%#v", deleted, status)
	}
}

func TestVXLANTunnelActiveReconcileDeletesStaleFloodFDB(t *testing.T) {
	router := bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}})
	store := &vxlanWhenStore{
		mapStore: mapStore{api.NetAPIVersion + "/VXLANTunnel/legacy-l2-overlay": {"managedBy": "routerd"}},
		values:   map[string]routerstate.Value{"ha.role": {Status: routerstate.StatusSet, Value: "master"}},
	}
	var commands []string
	stale := true
	dir := t.TempDir()
	writeVXLANOwnedArtifacts(t, dir, "vx-l2")
	controller := VXLANTunnelController{
		Router: router, DeclaredRouter: router, Store: store, NetworkdDir: dir, OperatingSystem: platform.OSLinux,
		Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
			joined := strings.Join(append([]string{name}, args...), " ")
			commands = append(commands, joined)
			switch {
			case strings.HasPrefix(joined, "ip -details link show dev vx-l2"):
				return []byte("8: vx-l2: <UP> mtu 1370 master br-l2\n vxlan id 200001 local 10.254.200.1 dev wg-l2 dstport 4789 nolearning"), nil
			case joined == "bridge fdb show dev vx-l2":
				if stale {
					return []byte("00:00:00:00:00:00 dst 10.254.200.2 self permanent\n00:00:00:00:00:00 dst 10.254.200.99 self permanent\n"), nil
				}
				return []byte("00:00:00:00:00:00 dst 10.254.200.2 self permanent\n"), nil
			case joined == "bridge fdb del 00:00:00:00:00:00 dev vx-l2 dst 10.254.200.99":
				stale = false
			}
			return nil, nil
		},
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !containsCommand(commands, "bridge fdb del 00:00:00:00:00:00 dev vx-l2 dst 10.254.200.99") {
		t.Fatalf("stale FDB was not removed: %#v", commands)
	}
	if containsCommand(commands, "bridge fdb del 00:00:00:00:00:00 dev vx-l2 dst 10.254.200.2") {
		t.Fatalf("desired FDB was removed: %#v", commands)
	}
	if phase := store.ObjectStatus(api.NetAPIVersion, "VXLANTunnel", "legacy-l2-overlay")["phase"]; phase != "Healthy" {
		t.Fatalf("phase=%v status=%#v", phase, store)
	}
}

func TestVXLANTunnelDualMasterRequiresSingleWitnessLeader(t *testing.T) {
	when := api.ResourceWhenSpec{All: []api.ResourceWhenSpec{
		{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}},
		{State: map[string]api.StateMatchSpec{"ha.lease": {Equals: "leader"}}},
	}}
	routerA := bridgeVXLANWhenRouter(when)
	routerB := bridgeVXLANWhenRouter(when)
	storeA := &vxlanWhenStore{mapStore: mapStore{api.NetAPIVersion + "/VXLANTunnel/legacy-l2-overlay": {"managedBy": "routerd"}}, values: map[string]routerstate.Value{
		"ha.role": {Status: routerstate.StatusSet, Value: "master"}, "ha.lease": {Status: routerstate.StatusSet, Value: "leader"},
	}}
	storeB := &vxlanWhenStore{mapStore: mapStore{api.NetAPIVersion + "/VXLANTunnel/legacy-l2-overlay": {"managedBy": "routerd"}}, values: map[string]routerstate.Value{
		"ha.role": {Status: routerstate.StatusSet, Value: "master"}, "ha.lease": {Status: routerstate.StatusSet, Value: "standby"},
	}}
	newController := func(store *vxlanWhenStore) VXLANTunnelController {
		dir := t.TempDir()
		writeVXLANOwnedArtifacts(t, dir, "vx-l2")
		exists := true
		return VXLANTunnelController{
			Router: routerA, DeclaredRouter: routerA, Store: store, NetworkdDir: dir, OperatingSystem: platform.OSLinux,
			Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
				joined := strings.Join(append([]string{name}, args...), " ")
				if strings.HasPrefix(joined, "ip -details link show dev vx-l2") {
					if !exists {
						return nil, errors.New("not found")
					}
					return []byte("8: vx-l2: <UP> mtu 1370 master br-l2\n vxlan id 200001 local 10.254.200.1 dev wg-l2 dstport 4789 nolearning"), nil
				}
				if joined == "ip link delete dev vx-l2" {
					exists = false
				}
				if joined == "bridge fdb show dev vx-l2" {
					return []byte("00:00:00:00:00:00 dst 10.254.200.2 self permanent\n"), nil
				}
				return nil, nil
			},
		}
	}
	controllerA := newController(storeA)
	controllerB := newController(storeB)
	controllerB.Router = routerB
	controllerB.DeclaredRouter = routerB
	var wg sync.WaitGroup
	for _, controller := range []VXLANTunnelController{controllerA, controllerB} {
		wg.Add(1)
		go func(current VXLANTunnelController) {
			defer wg.Done()
			if err := current.Reconcile(context.Background()); err != nil {
				t.Errorf("Reconcile: %v", err)
			}
		}(controller)
	}
	wg.Wait()
	forwarding := 0
	for _, store := range []*vxlanWhenStore{storeA, storeB} {
		if store.ObjectStatus(api.NetAPIVersion, "VXLANTunnel", "legacy-l2-overlay")["forwarding"] == true {
			forwarding++
		}
	}
	if forwarding != 1 {
		t.Fatalf("forwarding nodes = %d, want exactly one: A=%#v B=%#v", forwarding, storeA.mapStore, storeB.mapStore)
	}
}

func TestVXLANTunnelGateRejectsPersistedPreBootStateAndUnknownMatch(t *testing.T) {
	started := time.Now().UTC()
	resource := bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}}).Spec.Resources[3]
	store := &vxlanWhenStore{values: map[string]routerstate.Value{"ha.role": {Status: routerstate.StatusSet, Value: "master", UpdatedAt: started.Add(-time.Minute)}}}
	controller := VXLANTunnelController{Store: store, StartedAt: started}
	if got := controller.gateState(resource); got != vxlanGateUnknown {
		t.Fatalf("pre-boot gate=%s, want %s", got, vxlanGateUnknown)
	}
	store.values["ha.role"] = routerstate.Value{Status: routerstate.StatusSet, Value: "master", UpdatedAt: started.Add(time.Second)}
	if got := controller.gateState(resource); got != vxlanGateEnabled {
		t.Fatalf("fresh gate=%s, want %s", got, vxlanGateEnabled)
	}
	resource = bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "unknown"}}}).Spec.Resources[3]
	store.values["ha.role"] = routerstate.Value{Status: routerstate.StatusSet, Value: "unknown", UpdatedAt: started.Add(time.Second)}
	if got := controller.gateState(resource); got != vxlanGateUnknown {
		t.Fatalf("explicit unknown gate=%s, want fail-closed unknown", got)
	}
}

func TestVXLANTunnelRestartOrphanMarkerTriggersCleanup(t *testing.T) {
	dir := t.TempDir()
	controller := VXLANTunnelController{DeclaredRouter: &api.Router{}, Router: &api.Router{}, Store: mapStore{}, NetworkdDir: dir, OperatingSystem: platform.OSLinux}
	cfg := vxlan.Config{IfName: "vx-old", VNI: 200001, LocalAddress: "10.254.200.1", UnderlayInterface: "wg-l2", UDPPort: 4789, MTU: 1370, Bridge: "br-l2", Peers: []string{"10.254.200.2"}}
	if _, err := controller.persistOwnership(cfg); err != nil {
		t.Fatal(err)
	}
	exists := true
	controller.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		joined := strings.Join(append([]string{name}, args...), " ")
		if joined == "ip -details link show dev vx-old" {
			if !exists {
				return nil, errors.New("not found")
			}
			return []byte("8: vx-old: <UP> mtu 1370 master br-l2\n vxlan id 200001 local 10.254.200.1 dev wg-l2 dstport 4789 nolearning"), nil
		}
		if joined == "bridge fdb show dev vx-old" {
			return []byte("00:00:00:00:00:00 dst 10.254.200.2 self permanent\n"), nil
		}
		if joined == "ip link delete dev vx-old" {
			exists = false
		}
		return nil, nil
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("orphaned owned VXLAN still exists")
	}
	if _, err := os.Stat(controller.artifactPaths("vx-old")[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owner marker remains: %v", err)
	}
}

func TestVXLANTunnelWitnessLossBeforeCommitNeverBringsLinkUp(t *testing.T) {
	router := bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.lease": {Equals: "leader"}}})
	store := &vxlanWhenStore{mapStore: mapStore{}, values: map[string]routerstate.Value{"ha.lease": {Status: routerstate.StatusSet, Value: "leader"}}}
	dir := t.TempDir()
	exists := false
	upCalled := false
	fdbReads := 0
	controller := VXLANTunnelController{Router: router, DeclaredRouter: router, Store: store, NetworkdDir: dir, OperatingSystem: platform.OSLinux}
	controller.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		joined := strings.Join(append([]string{name}, args...), " ")
		if joined == "ip -details link show dev vx-l2" {
			if !exists {
				return nil, errors.New("not found")
			}
			return []byte("8: vx-l2: <BROADCAST> mtu 1370 master br-l2\n vxlan id 200001 local 10.254.200.1 dev wg-l2 dstport 4789 nolearning"), nil
		}
		if strings.HasPrefix(joined, "ip link add vx-l2 ") {
			exists = true
		}
		if joined == "bridge fdb show dev vx-l2" {
			fdbReads++
			if fdbReads == 1 {
				store.values["ha.lease"] = routerstate.Value{Status: routerstate.StatusSet, Value: "standby"}
			}
			return []byte("00:00:00:00:00:00 dst 10.254.200.2 self permanent\n"), nil
		}
		if joined == "ip link set dev vx-l2 up" {
			upCalled = true
		}
		if joined == "ip link delete dev vx-l2" {
			exists = false
		}
		return nil, nil
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if upCalled || exists {
		t.Fatalf("witness loss committed forwarding: up=%t exists=%t", upCalled, exists)
	}
	if phase := store.ObjectStatus(api.NetAPIVersion, "VXLANTunnel", "legacy-l2-overlay")["phase"]; phase != "Disabled" {
		t.Fatalf("phase=%v", phase)
	}
}

func TestVXLANTunnelOwnedConfigChangeDownsOldBeforeRecreate(t *testing.T) {
	router := bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}})
	resource := router.Spec.Resources[3]
	desired, _ := resource.VXLANTunnelSpec()
	desired.VNI = 200002
	resource.Spec = desired
	router.Spec.Resources[3] = resource
	store := &vxlanWhenStore{mapStore: mapStore{}, values: map[string]routerstate.Value{"ha.role": {Status: routerstate.StatusSet, Value: "master"}}}
	dir := t.TempDir()
	controller := VXLANTunnelController{Router: router, DeclaredRouter: router, Store: store, NetworkdDir: dir, OperatingSystem: platform.OSLinux}
	old := vxlan.Config{IfName: "vx-l2", VNI: 200001, LocalAddress: "10.254.200.1", UnderlayInterface: "wg-l2", UDPPort: 4789, MTU: 1370, Bridge: "br-l2", Peers: []string{"10.254.200.2"}}
	if _, err := controller.persistOwnership(old); err != nil {
		t.Fatal(err)
	}
	exists := true
	vni := 200001
	var commands []string
	controller.Command = func(_ context.Context, name string, args ...string) ([]byte, error) {
		joined := strings.Join(append([]string{name}, args...), " ")
		commands = append(commands, joined)
		if joined == "ip -details link show dev vx-l2" {
			if !exists {
				return nil, errors.New("not found")
			}
			return []byte(fmt.Sprintf("8: vx-l2: <UP> mtu 1370 master br-l2\n vxlan id %d local 10.254.200.1 dev wg-l2 dstport 4789 nolearning", vni)), nil
		}
		if joined == "ip link delete dev vx-l2" {
			exists = false
		}
		if strings.HasPrefix(joined, "ip link add vx-l2 type vxlan id 200002 ") {
			exists = true
			vni = 200002
		}
		if joined == "bridge fdb show dev vx-l2" {
			return []byte("00:00:00:00:00:00 dst 10.254.200.2 self permanent\n"), nil
		}
		return nil, nil
	}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	assertCommandsInOrder(t, commands, []string{"ip link set dev vx-l2 down", "ip link delete dev vx-l2", "ip link add vx-l2 type vxlan id 200002 local 10.254.200.1 dev wg-l2 dstport 4789 nolearning"})
}

func TestVXLANTunnelNextExpiryUsesProducerAbsoluteDeadline(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	router := bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master", MaxAge: "5s"}}})
	store := &vxlanWhenStore{now: now, values: map[string]routerstate.Value{"ha.role": {Status: routerstate.StatusSet, Value: "master", UpdatedAt: now.Add(-2 * time.Second)}}}
	c := VXLANTunnelController{DeclaredRouter: router, Store: store}
	if got := c.NextExpiryAfter(); got != 3*time.Second {
		t.Fatalf("NextExpiryAfter=%s want 3s", got)
	}
}

func TestVXLANTunnelDependencyRevisionChangesWithoutPredicateFlip(t *testing.T) {
	now := time.Now().UTC()
	router := bridgeVXLANWhenRouter(api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master"}}})
	resource := router.Spec.Resources[3]
	store := &vxlanWhenStore{now: now, values: map[string]routerstate.Value{"ha.role": {Status: routerstate.StatusSet, Value: "master", UpdatedAt: now}}}
	c := VXLANTunnelController{Store: store}
	first, ok := c.dependencyRevision(resource)
	if !ok {
		t.Fatal("missing first revision")
	}
	store.values["ha.role"] = routerstate.Value{Status: routerstate.StatusSet, Value: "master", UpdatedAt: now.Add(time.Nanosecond)}
	second, ok := c.dependencyRevision(resource)
	if !ok {
		t.Fatal("missing second revision")
	}
	if first == second {
		t.Fatal("producer revision did not change when timestamp changed")
	}
}

func TestVXLANTunnelOwnershipRejectsSameNameReplacementIfindex(t *testing.T) {
	router := bridgeVXLANTestRouter(true)
	resource := router.Spec.Resources[3]
	spec, _ := resource.VXLANTunnelSpec()
	cfg := vxlan.Config{Name: resource.Metadata.Name, IfName: spec.IfName, VNI: spec.VNI, LocalAddress: spec.LocalAddress, Peers: spec.Peers, UnderlayInterface: spec.UnderlayInterface, UDPPort: spec.UDPPort, MTU: spec.MTU, Bridge: spec.Bridge}
	c := VXLANTunnelController{NetworkdDir: t.TempDir()}
	if _, err := c.persistOwnershipBound(resource, cfg, 8); err != nil {
		t.Fatal(err)
	}
	c.Command = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("9: vx-l2: <UP> mtu 1370 master br-l2\n vxlan id 200001 local 10.254.200.1 dev wg-l2 dstport 4789 nolearning"), nil
	}
	if err := c.verifyOwnership(context.Background(), resource, cfg); err == nil {
		t.Fatal("same-name replacement with a new ifindex was accepted")
	}
}

func bridgeVXLANWhenRouter(when api.ResourceWhenSpec) *api.Router {
	router := bridgeVXLANTestRouter(true)
	for i, resource := range router.Spec.Resources {
		if resource.Kind != "VXLANTunnel" {
			continue
		}
		spec, _ := resource.VXLANTunnelSpec()
		spec.When = when
		resource.Spec = spec
		router.Spec.Resources[i] = resource
	}
	return router
}

func writeVXLANOwnedArtifacts(t *testing.T, dir, ifname string) {
	t.Helper()
	path := filepath.Join(dir, "31-routerd-"+ifname+".owner")
	if err := os.WriteFile(path, []byte(routerdGeneratedNetworkdHeader+"\n"), 0o600); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
}

func assertCommandsInOrder(t *testing.T, commands, wanted []string) {
	t.Helper()
	index := 0
	for _, command := range commands {
		if index < len(wanted) && command == wanted[index] {
			index++
		}
	}
	if index != len(wanted) {
		t.Fatalf("commands not in order; matched %d/%d: commands=%#v wanted=%#v", index, len(wanted), commands, wanted)
	}
}

func containsCommand(commands []string, wanted string) bool {
	for _, command := range commands {
		if command == wanted {
			return true
		}
	}
	return false
}

func bridgeVXLANTestRouter(withVXLAN bool) *api.Router {
	resources := []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "legacy-lan"}, Spec: api.InterfaceSpec{IfName: "ens19", Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "legacy-l2"}, Spec: api.BridgeSpec{IfName: "br-l2", Members: []string{"legacy-lan"}, MTU: 1370}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-l2"}, Spec: api.WireGuardInterfaceSpec{IfName: "wg-l2"}},
	}
	if withVXLAN {
		resources = append(resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "legacy-l2-overlay"}, Spec: api.VXLANTunnelSpec{IfName: "vx-l2", VNI: 200001, LocalAddress: "10.254.200.1", Peers: []string{"10.254.200.2"}, UnderlayInterface: "wg-l2", UDPPort: 4789, MTU: 1370, Bridge: "legacy-l2"}})
	}
	return &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: resources}}
}
