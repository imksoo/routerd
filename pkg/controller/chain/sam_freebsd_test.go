// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package chain

import (
	"net"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

func TestFreeBSDPublishedARPRouteMessageUsesExactEthernetIfindex(t *testing.T) {
	iface := &net.Interface{Index: 17, Name: "em0", HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 0x55}}
	message, sequence, err := freeBSDPublishedARPRouteMessage("192.0.2.55", iface, freeBSDIFTether)
	if err != nil {
		t.Fatalf("freeBSDPublishedARPRouteMessage: %v", err)
	}
	headerLen := int(unsafe.Sizeof(unix.RtMsghdr{}))
	rtm := (*unix.RtMsghdr)(unsafe.Pointer(&message[0]))
	if rtm.Type != unix.RTM_ADD || rtm.Seq != sequence || rtm.Pid == 0 {
		t.Fatalf("route header = %#v, sequence=%d", rtm, sequence)
	}
	wantFlags := int32(unix.RTF_HOST | unix.RTF_STATIC | unix.RTF_LLDATA | unix.RTF_PROTO2)
	if rtm.Flags != wantFlags || rtm.Addrs != unix.RTA_DST|unix.RTA_GATEWAY {
		t.Fatalf("route flags/addrs = %#x/%#x", rtm.Flags, rtm.Addrs)
	}
	if rtm.Inits != unix.RTV_EXPIRE {
		t.Fatalf("route inits = %#x, want RTV_EXPIRE", rtm.Inits)
	}
	dst := (*unix.RawSockaddrInet4)(unsafe.Pointer(&message[headerLen]))
	if dst.Family != unix.AF_INET || dst.Addr != [4]byte{192, 0, 2, 55} {
		t.Fatalf("route destination = %#v", dst)
	}
	gatewayOffset := headerLen + freeBSDSARPSockaddrAlign(int(unsafe.Sizeof(unix.RawSockaddrInet4{})))
	gateway := (*unix.RawSockaddrDatalink)(unsafe.Pointer(&message[gatewayOffset]))
	if gateway.Len != uint8(unsafe.Sizeof(unix.RawSockaddrDatalink{})) || gateway.Family != unix.AF_LINK || gateway.Index != 17 || gateway.Type != freeBSDIFTether || gateway.Alen != 6 {
		t.Fatalf("route gateway = %#v", gateway)
	}
	if got := unsafe.Slice((*byte)(unsafe.Pointer(&gateway.Data[0])), 6); net.HardwareAddr(got).String() != "02:00:00:00:00:55" {
		t.Fatalf("route gateway MAC = %x", got)
	}
}
