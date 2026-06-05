// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net"
	"net/netip"
	"strings"
	"testing"
)

func TestParseEthernetARPReply(t *testing.T) {
	frame := buildARPReplyForTest("02:00:00:00:00:11", "192.168.123.129", "02:00:00:00:00:22", "192.168.123.208")
	packet, ok, err := parseEthernetARP(frame)
	if err != nil {
		t.Fatalf("parseEthernetARP: %v", err)
	}
	if !ok {
		t.Fatal("parseEthernetARP ok=false")
	}
	if packet.Operation != arpReply {
		t.Fatalf("operation = %d, want arpReply", packet.Operation)
	}
	if got := packet.SenderIP.String(); got != "192.168.123.129" {
		t.Fatalf("sender IP = %s", got)
	}
	if got := packet.SenderMAC.String(); got != "02:00:00:00:00:11" {
		t.Fatalf("sender MAC = %s", got)
	}
	if got := packet.TargetIP.String(); got != "192.168.123.208" {
		t.Fatalf("target IP = %s", got)
	}
}

func TestBuildARPRequestUsesZeroSourceWhenDHCPAddressUnknown(t *testing.T) {
	mac := mustMAC(t, "02:00:00:00:00:aa")
	target := netip.MustParseAddr("192.168.123.132")
	frame := buildARPRequest(mac, netip.Addr{}, target)
	packet, ok, err := parseEthernetARP(frame)
	if err != nil {
		t.Fatalf("parseEthernetARP: %v", err)
	}
	if !ok {
		t.Fatal("parseEthernetARP ok=false")
	}
	if packet.Operation != arpRequest {
		t.Fatalf("operation = %d, want arpRequest", packet.Operation)
	}
	if got := packet.SenderIP.String(); got != "0.0.0.0" {
		t.Fatalf("sender IP = %s, want 0.0.0.0", got)
	}
	if got := packet.TargetIP.String(); got != "192.168.123.132" {
		t.Fatalf("target IP = %s", got)
	}
}

func TestParseOptionsDefaultsSourceModes(t *testing.T) {
	opts, err := parseOptions("test", []string{"--interface", "eth1", "--pool", "svnet1", "--prefix", "192.168.123.0/24", "--source-type", sourceOnDemandARP})
	if err != nil {
		t.Fatalf("parseOptions: %v", err)
	}
	if !opts.onDemand || opts.observe {
		t.Fatalf("on-demand defaults = observe:%v onDemand:%v, want observe:false onDemand:true", opts.observe, opts.onDemand)
	}
	opts, err = parseOptions("test", []string{"--interface", "eth1", "--pool", "svnet1", "--prefix", "192.168.123.0/24", "--source-type", sourcePVESVNet})
	if err != nil {
		t.Fatalf("parseOptions pve-svnet: %v", err)
	}
	if !opts.observe || opts.onDemand {
		t.Fatalf("pve-svnet defaults = observe:%v onDemand:%v, want observe:true onDemand:false", opts.observe, opts.onDemand)
	}
}

func TestParseARPTableKeepsCompleteIPv4Entries(t *testing.T) {
	entries := parseARPTable(strings.NewReader(`IP address       HW type     Flags       HW address            Mask     Device
192.168.123.129  0x1         0x2         02:00:00:00:00:11     *        vmbr123
192.168.123.130  0x1         0x0         02:00:00:00:00:12     *        vmbr123
192.168.123.131  0x1         0x2         00:00:00:00:00:00     *        vmbr123
fe80::1          0x1         0x2         02:00:00:00:00:13     *        vmbr123
`))
	if len(entries) != 1 {
		t.Fatalf("entries = %#v, want one complete IPv4 entry", entries)
	}
	if entries[0].IP.String() != "192.168.123.129" || entries[0].MAC.String() != "02:00:00:00:00:11" || entries[0].Device != "vmbr123" {
		t.Fatalf("entry = %#v", entries[0])
	}
	if !arpTableDeviceMatches("vmbr123", "eth1", "svnet1", "vmbr123") {
		t.Fatal("arpTableDeviceMatches did not match bridge")
	}
}

func buildARPReplyForTest(senderMAC, senderIP, targetMAC, targetIP string) []byte {
	srcMAC := mustMACForTest(senderMAC)
	dstMAC := mustMACForTest(targetMAC)
	srcIP := netip.MustParseAddr(senderIP).As4()
	dstIP := netip.MustParseAddr(targetIP).As4()
	frame := make([]byte, 42)
	copy(frame[0:6], dstMAC)
	copy(frame[6:12], srcMAC)
	frame[12], frame[13] = 0x08, 0x06
	frame[14], frame[15] = 0x00, 0x01
	frame[16], frame[17] = 0x08, 0x00
	frame[18], frame[19] = 6, 4
	frame[20], frame[21] = 0x00, arpReply
	copy(frame[22:28], srcMAC)
	copy(frame[28:32], srcIP[:])
	copy(frame[32:38], dstMAC)
	copy(frame[38:42], dstIP[:])
	return frame
}

func mustMAC(t *testing.T, value string) net.HardwareAddr {
	t.Helper()
	mac, err := net.ParseMAC(value)
	if err != nil {
		t.Fatal(err)
	}
	return mac
}

func mustMACForTest(value string) net.HardwareAddr {
	mac, err := net.ParseMAC(value)
	if err != nil {
		panic(err)
	}
	return mac
}
