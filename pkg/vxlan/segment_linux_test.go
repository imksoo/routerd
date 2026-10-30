// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package vxlan

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

func TestLinuxSegmentReadbackAcceptsNativeWildcardSource(t *testing.T) {
	for _, local := range []string{"0.0.0.0", "::"} {
		for _, source := range []net.IP{nil, net.ParseIP(local)} {
			t.Run(local+"/"+source.String(), func(t *testing.T) {
				config, owner, native := wildcardSegmentFixture(t, local)
				native.SrcAddr = source
				observed, err := linuxSegmentAttributes(owner.Scope, native)
				if err != nil {
					t.Fatal(err)
				}
				if !observed.LocalWildcard || observed.Config.Local != "" {
					t.Fatalf("native wildcard observation = %#v", observed)
				}
				if err := CheckSegmentReadback(config, owner, observed); err != nil {
					t.Fatalf("kernel-omitted wildcard source rejected: %v", err)
				}
			})
		}
	}
}

func TestLinuxSegmentWildcardDoesNotAdoptConcreteOrUnknownSource(t *testing.T) {
	config, owner, native := wildcardSegmentFixture(t, "0.0.0.0")
	native.SrcAddr = net.ParseIP("192.0.2.10")
	observed, err := linuxSegmentAttributes(owner.Scope, native)
	if err != nil {
		t.Fatal(err)
	}
	if observed.LocalWildcard {
		t.Fatal("concrete native source was classified as wildcard")
	}
	if err := CheckSegmentReadback(config, owner, observed); err == nil {
		t.Fatal("wildcard intent adopted a changed concrete source")
	}
	config.Local = "192.0.2.10"
	if err := CheckSegmentReadback(config, owner, observed); err != nil {
		t.Fatalf("exact concrete source rejected: %v", err)
	}

	native.SrcAddr = nil
	observed, err = linuxSegmentAttributes(owner.Scope, native)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckSegmentReadback(config, owner, observed); err == nil {
		t.Fatal("concrete source intent adopted native wildcard")
	}
	config.Local = "0.0.0.0"
	observed.LocalWildcard = false
	if err := CheckSegmentReadback(config, owner, observed); err == nil {
		t.Fatal("missing source observation proved wildcard ownership")
	}
}

func wildcardSegmentFixture(t *testing.T, local string) (SegmentConfig, SegmentIdentity, *netlink.Vxlan) {
	t.Helper()
	group := "239.1.1.1"
	if net.ParseIP(local).To4() == nil {
		group = "ff05::1"
	}
	config := SegmentConfig{IfName: "vxlan100", VNI: 100, Local: local, Group: group, Underlay: "underlay0", Port: 4789, MTU: 1400}
	owner := SegmentIdentity{Scope: "test-kernel", Token: "owned", Index: 10, CreationName: config.IfName, UnderlayIndex: 3}
	// vxlan_fill_info omits IFLA_VXLAN_LOCAL/LOCAL6 for wildcard sources.
	// Decode that actual message shape with the same upstream decoder as GETLINK.
	msg := nl.NewIfInfomsg(unix.AF_UNSPEC)
	msg.Index, msg.Flags = int32(owner.Index), unix.IFF_UP|unix.IFF_RUNNING
	wire := append([]byte(nil), msg.Serialize()...)
	for _, attr := range []*nl.RtAttr{
		nl.NewRtAttr(unix.IFLA_IFNAME, nl.ZeroTerminated(config.IfName)),
		nl.NewRtAttr(unix.IFLA_IFALIAS, nl.ZeroTerminated(SegmentProof(owner.Token))),
		nl.NewRtAttr(unix.IFLA_MTU, nl.Uint32Attr(uint32(config.MTU))),
	} {
		wire = append(wire, attr.Serialize()...)
	}
	info := nl.NewRtAttr(unix.IFLA_LINKINFO, nil)
	info.AddRtAttr(nl.IFLA_INFO_KIND, nl.ZeroTerminated("vxlan"))
	data := info.AddRtAttr(nl.IFLA_INFO_DATA, nil)
	data.AddRtAttr(nl.IFLA_VXLAN_LEARNING, []byte{1})
	data.AddRtAttr(nl.IFLA_VXLAN_ID, nl.Uint32Attr(uint32(config.VNI)))
	data.AddRtAttr(nl.IFLA_VXLAN_LINK, nl.Uint32Attr(uint32(owner.UnderlayIndex)))
	data.AddRtAttr(nl.IFLA_VXLAN_PORT, binary.BigEndian.AppendUint16(nil, uint16(config.Port)))
	if ip := net.ParseIP(group); ip.To4() != nil {
		data.AddRtAttr(nl.IFLA_VXLAN_GROUP, ip.To4())
	} else {
		data.AddRtAttr(nl.IFLA_VXLAN_GROUP6, ip.To16())
	}
	wire = append(wire, info.Serialize()...)
	decoded, err := netlink.LinkDeserialize(nil, wire)
	if err != nil {
		t.Fatal(err)
	}
	return config, owner, decoded.(*netlink.Vxlan)
}
