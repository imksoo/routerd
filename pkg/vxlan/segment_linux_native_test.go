// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package vxlan

import (
	"context"
	"net"
	"os"
	"os/exec"
	"testing"

	"github.com/imksoo/routerd/pkg/platform"
	"github.com/vishvananda/netlink"
)

func TestLinuxSegmentNativeCreateInstallsOwnershipCookie(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("set ROUTERD_RUN_NETNS=1 for isolated native VXLAN lifecycle")
	}
	namespace, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	const childEnvironment = "ROUTERD_VXLAN_NATIVE_PARENT_NAMESPACE"
	if parent := os.Getenv(childEnvironment); parent == "" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		// The new network and mount namespaces contain every mutation,
		// including sysfs. No caller's network links or mounts are changed.
		command := exec.CommandContext(t.Context(), "unshare", "-Urnm", "sh", "-c", `mount -t sysfs sysfs /sys && exec "$@"`, "vxlan-native", executable, "-test.run=^TestLinuxSegmentNativeCreateInstallsOwnershipCookie$", "-test.v")
		command.Env = append(os.Environ(), childEnvironment+"="+namespace)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("isolated native VXLAN lifecycle failed: %v\n%s", err, output)
		}
		t.Logf("%s", output)
		return
	} else if parent == namespace {
		t.Fatal("native VXLAN test did not enter an isolated network namespace")
	}

	handle, err := segmentNetlink(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	loopback, err := handle.LinkByName("lo")
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.LinkSetUp(loopback); err != nil {
		t.Fatal(err)
	}
	native := linuxSegmentNative{scope: platform.KernelNetworkScope}
	config := SegmentConfig{IfName: "vx-proof", VNI: 4242, Local: "127.0.0.1", Underlay: "lo", Port: 4789, MTU: 1400, NoLearning: true, OuterDF: "inherit", Remotes: []string{"127.0.0.2"}}
	owner, err := native.Resolve(t.Context(), config, "00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	owner.Pending = true
	if err := native.Create(t.Context(), config, owner); err != nil {
		t.Fatal(err)
	}
	observed, err := native.ObserveOwned(t.Context(), config, owner)
	if err != nil || CheckSegmentIdentity(config, owner, observed) != nil {
		t.Fatalf("successful native create did not establish its ownership cookie: observed=%+v err=%v", observed, err)
	}
	owner.Index = observed.Index
	if err := native.Configure(t.Context(), config, owner, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	observed, err = native.ObserveOwned(t.Context(), config, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckSegmentReadback(config, owner, observed); err != nil {
		t.Fatalf("native VXLAN activation readback failed: %+v %v", observed, err)
	}
	if err := native.Remove(t.Context(), config, owner); err != nil {
		t.Fatal(err)
	}
	if observed, err := native.Observe(t.Context(), config.IfName); err != nil || observed.Exists {
		t.Fatalf("native VXLAN withdrawal was not observed: %+v %v", observed, err)
	}

	// A matching name and tunnel tuple without this create's exclusive ACK
	// cannot authorize installing the private cookie on another native link.
	foreign := &netlink.Vxlan{LinkAttrs: netlink.LinkAttrs{Name: config.IfName, MTU: config.MTU}, VxlanId: config.VNI, VtepDevIndex: loopback.Attrs().Index, SrcAddr: net.ParseIP(config.Local), Port: config.Port}
	if err := handle.LinkAdd(foreign); err != nil {
		t.Fatal(err)
	}
	if err := handle.LinkSetAlias(foreign, "operator"); err != nil {
		t.Fatal(err)
	}
	owner.Index = 0
	if err := native.Create(t.Context(), config, owner); err == nil {
		t.Fatal("failed exclusive create adopted an existing VXLAN")
	}
	remaining, err := handle.LinkByName(config.IfName)
	if err != nil || remaining.Attrs().Alias != "operator" {
		t.Fatalf("failed exclusive create changed foreign ownership: %+v %v", remaining, err)
	}
}
