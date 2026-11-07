// SPDX-License-Identifier: BSD-3-Clause

package vxlan

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"testing"
	"unsafe"
)

const bsdSegmentTestScope = "freebsd:1700000000:123:7"

func TestBSDSegmentDriverABIDecodesNativePortsAddressesAndMulticastIndex(t *testing.T) {
	var raw bsdVXLANDriverConfig
	if unsafe.Sizeof(raw) != 84 || unsafe.Offsetof(raw.Local) != 4 || unsafe.Offsetof(raw.Remote) != 32 || unsafe.Offsetof(raw.MCIndex) != 60 || unsafe.Offsetof(raw.Learning) != 80 {
		t.Fatal("Go VXLAN driver layout differs from native if_vxlancfg")
	}
	raw.VNI, raw.MCIndex = 100, 4
	raw.Local = [28]byte{16, 2, 0x12, 0xb5, 192, 0, 2, 1}
	raw.Remote = [28]byte{16, 2, 0x12, 0xb5, 239, 1, 2, 3}
	got, err := decodeBSDSegmentConfig(raw)
	if err != nil || got.Local.String() != "192.0.2.1" || got.Remote.String() != "239.1.2.3" || got.LocalPort != 4789 || got.RemotePort != 4789 || got.MCIndex != 4 {
		t.Fatalf("native driver observation=%+v %v", got, err)
	}
	raw.Local = [28]byte{28, 28, 0x12, 0xb5, 0, 0, 0, 0, 0xfe, 0x80, 0, 7, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xab, 0xcd}
	got, err = decodeBSDSegmentConfig(raw)
	if err != nil || got.Local.String() != "fe80::abcd" {
		t.Fatalf("embedded native IPv6 scope was treated as address bits: %+v %v", got, err)
	}
}

func TestBSDSegmentNativeGuardUsesDriverMulticastIndexAndBothPorts(t *testing.T) {
	for _, cause := range []string{"multicast device", "local port", "foreign cookie", "inventory failure"} {
		t.Run(cause, func(t *testing.T) {
			token := strings.Repeat("a", 32)
			proof := SegmentProof(token)
			if cause == "foreign cookie" {
				proof = "operator interface"
			}
			output := "em0: flags=8843<UP,BROADCAST,RUNNING> metric 0 mtu 1500\n\tinet 192.0.2.1 netmask 0xffffff00\nem1: flags=8843<UP,BROADCAST,RUNNING> metric 0 mtu 1500\nbridge0: flags=8843<UP,BROADCAST,RUNNING> metric 0 mtu 1500\n\tgroups: bridge\n\tmember: vxlan100 flags=143<LEARNING,DISCOVER,AUTOEDGE,AUTOPTP>\nvxlan100: flags=8843<UP,BROADCAST,RUNNING> metric 0 mtu 1400\n\tdescription: " + proof + "\n\tgroups: vxlan\n"
			cfg := bsdSegmentConfig{VNI: 100, Local: netip.MustParseAddr("192.0.2.1"), Remote: netip.MustParseAddr("239.1.2.3"), LocalPort: 4789, RemotePort: 4789, MCIndex: 2}
			if cause == "multicast device" {
				cfg.MCIndex = 4
			}
			if cause == "local port" {
				cfg.LocalPort = 4790
			}
			var effects []string
			native := freeBSDSegmentNative{scope: func(context.Context) (string, error) { return bsdSegmentTestScope, nil }, command: func(_ context.Context, _ string, args ...string) ([]byte, error) {
				if len(args) == 1 && args[0] == "-a" {
					return []byte(output), nil
				}
				effects = append(effects, strings.Join(args, " "))
				return nil, nil
			}, interfaces: func() ([]net.Interface, error) {
				if cause == "inventory failure" {
					return nil, errors.New("native inventory unreadable")
				}
				return []net.Interface{{Name: "em0", Index: 2, MTU: 1500}, {Name: "em1", Index: 4, MTU: 1500}, {Name: "bridge0", Index: 3, MTU: 1500}, {Name: "vxlan100", Index: 8, MTU: 1400, Flags: net.FlagUp | net.FlagRunning}}, nil
			}, readConfig: func(context.Context, string, int) (bsdSegmentConfig, error) { return cfg, nil }}
			plan := SegmentConfig{IfName: "vxlan100", VNI: 100, Local: "192.0.2.1", Group: "239.1.2.3", Underlay: "em0", Port: 4789, MTU: 1400, Bridge: "bridge0"}
			owner := SegmentIdentity{Scope: bsdSegmentTestScope, Token: token, CreationName: "vxlan100", Index: 8, UnderlayIndex: 2, BridgeIndex: 3}
			if err := native.Remove(t.Context(), plan, owner); err == nil || len(effects) != 0 {
				t.Fatalf("unknown/foreign tunnel tuple allowed native removal: effects=%v err=%v", effects, err)
			}
		})
	}
}

func TestBSDSegmentExactCloneRenameAdmissionAndIdempotentBridgeAttachment(t *testing.T) {
	plan := SegmentConfig{IfName: "tenant-vx", VNI: 100, Local: "192.0.2.1", Remotes: []string{"192.0.2.2"}, Underlay: "em0", Port: 4789, MTU: 1400, Bridge: "bridge0"}
	token := strings.Repeat("b", 32)
	name, exists, attached, up := "", false, false, false
	var commands []string
	base := "em0: flags=8843<UP,BROADCAST,RUNNING> metric 0 mtu 1500\n\tinet 192.0.2.1 netmask 0xffffff00\nbridge0: flags=8843<UP,BROADCAST,RUNNING> metric 0 mtu 1500\n\tgroups: bridge\n"
	native := freeBSDSegmentNative{scope: func(context.Context) (string, error) { return bsdSegmentTestScope, nil }, interfaces: func() ([]net.Interface, error) {
		interfaces := []net.Interface{{Name: "em0", Index: 2, MTU: 1500}, {Name: "bridge0", Index: 3, MTU: 1500}}
		if exists {
			flags := net.Flags(0)
			if up {
				flags = net.FlagUp | net.FlagRunning
			}
			interfaces = append(interfaces, net.Interface{Name: name, Index: 8, MTU: 1400, Flags: flags})
		}
		return interfaces, nil
	}, readConfig: func(_ context.Context, current string, index int) (bsdSegmentConfig, error) {
		if !exists || current != name || index != 8 {
			return bsdSegmentConfig{}, errors.New("native index changed")
		}
		return bsdSegmentConfig{VNI: 100, Local: netip.MustParseAddr("192.0.2.1"), Remote: netip.MustParseAddr("192.0.2.2"), LocalPort: 4789, RemotePort: 4789}, nil
	}}
	native.command = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) == 1 && args[0] == "-a" {
			output := base
			if attached {
				output += "\tmember: " + name + " flags=143<LEARNING,DISCOVER>\n"
			}
			if exists {
				output += fmt.Sprintf("%s: flags=8802<BROADCAST,SIMPLEX,MULTICAST> metric 0 mtu 1400\n\tdescription: %s\n\tgroups: vxlan\n", name, SegmentProof(token))
			}
			return []byte(output), nil
		}
		commands = append(commands, strings.Join(args, " "))
		if len(args) >= 2 && args[1] == "create" {
			exists, name = true, args[0]
		} else if len(args) == 3 && args[1] == "name" {
			name = args[2]
		} else if len(args) == 3 && args[1] == "addm" {
			if attached {
				return nil, errors.New("already a bridge member")
			}
			attached = true
		} else if len(args) == 2 && args[1] == "up" {
			up = true
		} else if len(args) >= 2 && args[1] == "down" {
			up = false
		}
		return nil, nil
	}
	owner, err := native.Resolve(t.Context(), plan, token)
	if err != nil {
		t.Fatal(err)
	}
	owner.Pending = true
	if owner.CreationName == plan.IfName || !strings.HasPrefix(owner.CreationName, "vxlan") {
		t.Fatalf("arbitrary desired name did not receive an exact native creation name: %+v", owner)
	}
	if err := native.Create(t.Context(), plan, owner); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || !strings.HasPrefix(commands[0], owner.CreationName+" create description "+SegmentProof(token)) || strings.Contains(commands[0], " name ") {
		t.Fatalf("creation used an unrecorded anonymous clone/rename: %v", commands)
	}
	owner.Index = 8 // The actor binds this before configure/rename.
	failure := errors.New("current intent unavailable")
	if err := native.Configure(t.Context(), plan, owner, func(context.Context) error { return failure }); !errors.Is(err, failure) || up {
		t.Fatalf("failed admission activated forwarding: up=%v err=%v", up, err)
	}
	if name != plan.IfName || !attached {
		t.Fatal("pending rename/port effects could not be observed for recovery")
	}
	if err := native.Configure(t.Context(), plan, owner, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	observed, err := native.ObserveOwned(t.Context(), plan, owner)
	if err != nil || CheckSegmentReadback(plan, owner, observed) != nil {
		t.Fatalf("native activation readback: %+v %v", observed, err)
	}
	owner.Pending, up = false, false
	if err := native.Configure(t.Context(), plan, owner, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("repair attempted to add an already attached port: %v", err)
	}
}
