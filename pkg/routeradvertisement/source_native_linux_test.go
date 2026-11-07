// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package routeradvertisement

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func TestListenRejectsTentativeLinkLocal(t *testing.T) {
	if os.Getenv("ROUTERD_RUN_NETNS") != "1" {
		t.Skip("requires explicit isolated network namespace")
	}
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		t.Fatal(err)
	}
	host := os.Getenv("ROUTERD_HOST_NETNS")
	if !strings.HasPrefix(host, "net:[") || current == host {
		t.Skip("network namespace isolation is unverified")
	}
	run := func(name string, args ...string) {
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v %s", name, args, err, out)
		}
	}
	run("ip", "link", "add", "ra-dad", "type", "veth", "peer", "name", "ra-peer")
	defer run("ip", "link", "del", "ra-dad")
	run("ip", "link", "set", "ra-dad", "addrgenmode", "none")
	run("sysctl", "-qw", "net.ipv6.conf.ra-dad.dad_transmits=10")
	run("ip", "link", "set", "ra-peer", "up")
	run("ip", "link", "set", "ra-dad", "up")
	run("ip", "-6", "address", "add", "fe80::1/64", "dev", "ra-dad")
	link, err := netlink.LinkByName("ra-dad")
	if err != nil {
		t.Fatal(err)
	}
	addresses, err := netlink.AddrList(link, netlink.FAMILY_V6)
	if err != nil {
		t.Fatal(err)
	}
	tentative := false
	for _, address := range addresses {
		if address.IP.String() == "fe80::1" && address.Flags&unix.IFA_F_TENTATIVE != 0 {
			tentative = true
		}
	}
	if !tentative {
		t.Fatal("fixture link-local address is not tentative")
	}
	endpoint, err := Listen("ra-dad")
	if err == nil {
		_ = endpoint.Conn.Close()
		t.Fatal("Listen admitted a tentative link-local transmit source")
	}
	if !errors.Is(err, ErrSourceTentative) {
		t.Fatalf("wrong readiness result: %v", err)
	}
	run("ip", "-6", "address", "del", "fe80::1/64", "dev", "ra-dad")
	run("ip", "-6", "address", "add", "fe80::1/64", "dev", "ra-dad", "nodad")
	endpoint, err = Listen("ra-dad")
	if err != nil {
		t.Fatal(err)
	}
	defer endpoint.Conn.Close()
	if endpoint.Source.String() != "fe80::1%ra-dad" {
		t.Fatalf("wrong ready source: %s", endpoint.Source)
	}
}
