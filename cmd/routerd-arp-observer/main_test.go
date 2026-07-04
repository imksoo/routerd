// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
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

func TestParseOptionsAcceptsRepeatedIgnoreSenderMAC(t *testing.T) {
	_, err := parseOptions("test", []string{
		"--interface", "eth1",
		"--pool", "svnet1",
		"--prefix", "192.168.123.0/24",
		"--ignore-sender-mac", "02:00:00:00:00:BB",
	})
	if err == nil || !strings.Contains(err.Error(), "flag provided but not defined: -ignore-sender-mac") {
		t.Fatalf("parseOptions --ignore-sender-mac error = %v, want unknown flag", err)
	}
}

func TestSetIgnoredSenderMACsCommandReplacesAndNormalizes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socket := filepath.Join(t.TempDir(), "arp.sock")
	d := &daemon{
		opts: options{
			resource:       "arp",
			ifname:         "eth1",
			eventInterface: "eth1",
			socketPath:     socket,
			eventFile:      filepath.Join(t.TempDir(), "events.jsonl"),
			poolName:       "svnet1",
			prefix:         netip.MustParsePrefix("192.168.123.0/24"),
		},
		startedAt:      time.Now(),
		cancel:         cancel,
		lastEventByKey: map[string]time.Time{},
		clients:        map[string]arpClient{},
	}
	d.cond = sync.NewCond(&d.mu)
	errc := make(chan error, 1)
	go func() { errc <- d.serve(ctx) }()
	waitForSocket(t, socket)

	result := postCommandForTest(t, socket, daemonapi.CommandRequest{
		Command: "set-ignored-sender-macs",
		Attributes: map[string]string{
			"macAddresses": "02:00:00:00:00:BB,02:00:00:00:00:aa",
		},
	})
	if !result.Accepted {
		t.Fatalf("set command rejected: %#v", result)
	}
	status := d.status()
	if got, want := status.Observed["ignoredSenderMACs"], "02:00:00:00:00:aa,02:00:00:00:00:bb"; got != want {
		t.Fatalf("ignoredSenderMACs = %q, want %q", got, want)
	}
	if got := status.Observed["ignoredSenderMACCount"]; got != "2" {
		t.Fatalf("ignoredSenderMACCount = %q, want 2", got)
	}

	result = postCommandForTest(t, socket, daemonapi.CommandRequest{
		Command: "set-ignored-sender-macs",
		Attributes: map[string]string{
			"macAddresses": "02:00:00:00:00:cc",
		},
	})
	if !result.Accepted {
		t.Fatalf("replacement command rejected: %#v", result)
	}
	status = d.status()
	if got, want := status.Observed["ignoredSenderMACs"], "02:00:00:00:00:cc"; got != want {
		t.Fatalf("ignoredSenderMACs after replacement = %q, want %q", got, want)
	}

	result = postCommandForTest(t, socket, daemonapi.CommandRequest{
		Command: "set-ignored-sender-macs",
		Attributes: map[string]string{
			"macAddresses": "not-a-mac",
		},
	})
	if result.Accepted {
		t.Fatalf("invalid MAC command accepted: %#v", result)
	}
	status = d.status()
	if got, want := status.Observed["ignoredSenderMACs"], "02:00:00:00:00:cc"; got != want {
		t.Fatalf("ignoredSenderMACs changed after rejected command = %q, want %q", got, want)
	}

	cancel()
	if err := <-errc; err != nil {
		t.Fatalf("serve: %v", err)
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

func TestNextIPv4PrefixProbeTargetWalksUsableHosts(t *testing.T) {
	prefix := netip.MustParsePrefix("192.168.123.0/30")
	source := netip.MustParseAddr("192.168.123.1")
	target, next, ok := nextIPv4PrefixProbeTarget(prefix, 0, source)
	if !ok {
		t.Fatal("nextIPv4PrefixProbeTarget ok=false")
	}
	if got := target.String(); got != "192.168.123.2" {
		t.Fatalf("target = %s, want first non-source usable host", got)
	}
	if next != 3 {
		t.Fatalf("next cursor = %d, want 3", next)
	}

	target, _, ok = nextIPv4PrefixProbeTarget(prefix, next, source)
	if !ok {
		t.Fatal("nextIPv4PrefixProbeTarget wrap ok=false")
	}
	if got := target.String(); got != "192.168.123.2" {
		t.Fatalf("wrapped target = %s, want only non-source usable host", got)
	}
}

func TestNextIPv4PrefixProbeTargetAllowsPointToPointHosts(t *testing.T) {
	prefix := netip.MustParsePrefix("10.255.0.20/31")
	target, next, ok := nextIPv4PrefixProbeTarget(prefix, 0, netip.Addr{})
	if !ok || target.String() != "10.255.0.20" {
		t.Fatalf("first target = %s ok=%v, want 10.255.0.20", target, ok)
	}
	target, _, ok = nextIPv4PrefixProbeTarget(prefix, next, netip.Addr{})
	if !ok || target.String() != "10.255.0.21" {
		t.Fatalf("second target = %s ok=%v, want 10.255.0.21", target, ok)
	}
}

func TestPublishObservationDemotesKnownSameIPMAC(t *testing.T) {
	d := &daemon{
		opts: options{
			resource:       "arp",
			ifname:         "eth1",
			eventInterface: "eth1",
			eventFile:      filepath.Join(t.TempDir(), "events.jsonl"),
			poolName:       "svnet1",
			prefix:         netip.MustParsePrefix("192.168.123.0/24"),
		},
		lastEventByKey: map[string]time.Time{},
		clients:        map[string]arpClient{},
	}
	d.cond = sync.NewCond(&d.mu)
	address := netip.MustParseAddr("192.168.123.10")
	mac := mustMAC(t, "02:00:00:00:00:10")
	d.publishObservation(address, mac, eventARPObserved, sourceARPObserver, "ARPObserved", "observed")
	if len(d.events) != 1 || d.events[0].Severity != daemonapi.SeverityInfo {
		t.Fatalf("first observation events = %#v, want one info event", d.events)
	}

	d.mu.Lock()
	d.lastEventByKey[eventARPObserved+"|"+address.String()+"|"+strings.ToLower(mac.String())] = time.Now().Add(-time.Minute)
	d.mu.Unlock()
	d.publishObservation(address, mac, eventARPObserved, sourceARPObserver, "ARPObserved", "observed")
	if len(d.events) != 2 || d.events[1].Severity != daemonapi.SeverityDebug {
		t.Fatalf("repeat observation events = %#v, want second debug event", d.events)
	}

	changedMAC := mustMAC(t, "02:00:00:00:00:11")
	d.publishObservation(address, changedMAC, eventARPObserved, sourceARPObserver, "ARPObserved", "observed")
	if len(d.events) != 3 || d.events[2].Severity != daemonapi.SeverityInfo {
		t.Fatalf("MAC change events = %#v, want third info event", d.events)
	}
}

func TestPublishObservationIgnoresSAMMemberMACAtChokepoint(t *testing.T) {
	memberMAC := mustMAC(t, "02:00:00:00:00:aa")
	memberMAC2 := mustMAC(t, "02:00:00:00:00:cc")
	clientMAC := mustMAC(t, "02:00:00:00:00:bb")
	d := &daemon{
		opts: options{
			resource:       "arp",
			ifname:         "eth1",
			eventInterface: "eth1",
			eventFile:      filepath.Join(t.TempDir(), "events.jsonl"),
			poolName:       "svnet1",
			prefix:         netip.MustParsePrefix("192.168.123.0/24"),
			ignoredSenderMACs: map[string]bool{
				strings.ToLower(memberMAC.String()):  true,
				strings.ToLower(memberMAC2.String()): true,
			},
		},
		lastEventByKey: map[string]time.Time{},
		clients:        map[string]arpClient{},
	}
	d.cond = sync.NewCond(&d.mu)
	address := netip.MustParseAddr("192.168.123.10")

	d.publishObservation(address, memberMAC, eventARPObserved, sourceARPObserver, "ARPObserved", "observed")
	if len(d.events) != 0 {
		t.Fatalf("member MAC observation events = %#v, want none", d.events)
	}
	if d.observedCount != 0 {
		t.Fatalf("observedCount = %d, want 0 for ignored member MAC", d.observedCount)
	}
	status := d.status()
	if status.Observed["ignoredSenderMACs"] != "02:00:00:00:00:aa,02:00:00:00:00:cc" || status.Observed["ignoredSenderMACCount"] != "2" || status.Observed["ignoredSenderMACObservationCount"] != "1" {
		t.Fatalf("status ignored sender MAC fields = %#v", status.Observed)
	}

	d.publishObservation(address, clientMAC, eventARPObserved, sourceARPObserver, "ARPObserved", "observed")
	if len(d.events) != 1 {
		t.Fatalf("client MAC observation events = %#v, want one event", d.events)
	}
	if got := d.events[0].Attributes["mac"]; got != strings.ToLower(clientMAC.String()) {
		t.Fatalf("client event mac = %q, want %s", got, clientMAC)
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", path)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s did not become ready", path)
}

func postCommandForTest(t *testing.T, socket string, req daemonapi.CommandRequest) daemonapi.CommandResult {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}}
	resp, err := client.Post("http://unix/v1/commands", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post command: %v", err)
	}
	defer resp.Body.Close()
	var result daemonapi.CommandResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode command result: %v", err)
	}
	return result
}
