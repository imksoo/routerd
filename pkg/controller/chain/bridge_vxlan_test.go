// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
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
		Store:           store,
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
				return []byte("8: vx-l2: <UP> mtu 1370 master br-l2\n    vxlan id 200001 local 10.254.200.1 dev wg-l2 dstport 4789 nolearning"), nil
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
