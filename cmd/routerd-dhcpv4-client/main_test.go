// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dhcpv4client"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRestoreLeaseIgnoresEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.json")
	if err := os.WriteFile(path, []byte(" \n\t"), 0644); err != nil {
		t.Fatalf("write lease: %v", err)
	}
	daemon := &dhcpv4Daemon{opts: options{leaseFile: path}}
	if err := daemon.restoreLease(); err != nil {
		t.Fatalf("restore empty lease: %v", err)
	}
}

func TestStopCommandCancelsTheDaemonOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	d := &dhcpv4Daemon{opts: options{leaseFile: filepath.Join(dir, "lease.json"), eventFile: filepath.Join(dir, "events.jsonl")}, cancel: cancel}
	d.cond = sync.NewCond(&d.mu)
	response := httptest.NewRecorder()
	d.handleCommand(response, httptest.NewRequest(http.MethodPost, "/v1/commands/stop", strings.NewReader("")))
	var result daemonapi.CommandResult
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || !result.Accepted || ctx.Err() != context.Canceled {
		t.Fatalf("stop did not cancel the worker: %+v", result)
	}
}

type wireConn struct {
	receive  chan []byte
	sent     chan dhcpv4client.Packet
	deadline time.Time
}

func (c *wireConn) Send(ctx context.Context, p dhcpv4client.Packet) error {
	select {
	case c.sent <- p:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (c *wireConn) SetReadDeadline(t time.Time) error { c.deadline = t; return nil }
func (c *wireConn) Close() error                      { return nil }
func (c *wireConn) ReadFromUDP(dst []byte) (int, *net.UDPAddr, error) {
	timer := time.NewTimer(time.Until(c.deadline))
	defer timer.Stop()
	select {
	case packet := <-c.receive:
		return copy(dst, packet), &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 67}, nil
	case <-timer.C:
		return 0, nil, os.ErrDeadlineExceeded
	}
}
func (c *wireConn) request(t *testing.T) dhcpv4client.Message {
	t.Helper()
	select {
	case packet := <-c.sent:
		msg, err := dhcpv4client.Decode(packet.Payload)
		if err != nil {
			t.Fatal(err)
		}
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not transmit")
		return dhcpv4client.Message{}
	}
}
func daemonReply(t *testing.T, request dhcpv4client.Message, kind byte) []byte {
	t.Helper()
	msg := dhcpv4client.Message{Op: 2, HType: 1, XID: request.XID, CHAddr: request.CHAddr, YIAddr: netip.MustParseAddr("192.0.2.10"), Options: map[byte][]byte{
		dhcpv4client.OptionMessageType: {kind}, dhcpv4client.OptionServerID: {192, 0, 2, 1}, dhcpv4client.OptionSubnetMask: {255, 255, 255, 0}, dhcpv4client.OptionLeaseTime: {0, 0, 0, 120},
	}}
	packet, err := dhcpv4client.Encode(msg)
	if err != nil {
		t.Fatal(err)
	}
	return packet
}
func testDaemon(t *testing.T) (*dhcpv4Daemon, *wireConn, *time.Time) {
	t.Helper()
	dir := t.TempDir()
	now := time.Now().UTC()
	conn := &wireConn{receive: make(chan []byte, 8), sent: make(chan dhcpv4client.Packet, 128)}
	d := &dhcpv4Daemon{opts: options{resource: "wan", ifname: "eth0", leaseFile: filepath.Join(dir, "lease.json"), eventFile: filepath.Join(dir, "events.jsonl")}, conn: conn}
	d.cond = sync.NewCond(&d.mu)
	client, err := dhcpv4client.New(dhcpv4client.Config{Resource: "wan", Interface: "eth0", HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 255}, Now: func() time.Time { return now }}, conn)
	if err != nil {
		t.Fatal(err)
	}
	d.client = client
	return d, conn, &now
}
func bindDaemon(t *testing.T, d *dhcpv4Daemon, conn *wireConn) {
	t.Helper()
	discover := conn.request(t)
	conn.receive <- daemonReply(t, discover, dhcpv4client.MessageOffer)
	request := conn.request(t)
	conn.receive <- daemonReply(t, request, dhcpv4client.MessageACK)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		bound := d.snapshotLocked().State == dhcpv4client.StateBound
		d.mu.Unlock()
		if bound {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("worker did not bind server ACK")
}
func TestDaemonLoopUsesClientAndPersistsConfirmedLease(t *testing.T) {
	d, conn, _ := testDaemon(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.loop(ctx, true) }()
	bindDaemon(t, d, conn)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(d.opts.leaseFile)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot dhcpv4client.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.State != dhcpv4client.StateBound || snapshot.CurrentAddress != "192.0.2.10" || snapshot.HardwareAddress != "02:00:00:00:00:ff" {
		t.Fatalf("wrong persisted owner: %+v", snapshot)
	}
	restarted, _, _ := testDaemon(t)
	restarted.opts.leaseFile = d.opts.leaseFile
	if err := restarted.restoreLease(); err != nil {
		t.Fatal(err)
	}
	restarted.mu.Lock()
	s := restarted.snapshotLocked()
	restarted.mu.Unlock()
	if s.CurrentAddress != "" {
		t.Fatal("restore fabricated lease authority")
	}
}
func TestDaemonCommandsAndExpiryShareOwner(t *testing.T) {
	d, conn, now := testDaemon(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.loop(ctx, false) }()
	defer func() {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("worker stopped: %v", err)
		}
	}()
	bindDaemon(t, d, conn)
	d.mu.Lock()
	*now = d.snapshotLocked().RenewAt
	d.mu.Unlock()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response := httptest.NewRecorder()
			d.handleCommand(response, httptest.NewRequest(http.MethodPost, "/v1/commands/renew", nil))
			if response.Code != http.StatusOK {
				t.Errorf("renew: %s", response.Body.String())
			}
			d.handleStatus(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/status", nil))
		}()
	}
	wg.Wait()
	renew := conn.request(t)
	if renew.CIAddr.String() != "192.0.2.10" {
		t.Fatal("command bypassed renewing client exchange")
	}
	response := httptest.NewRecorder()
	d.handleStatus(response, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	var status daemonapi.DaemonStatus
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if resource := status.Resources[0]; resource.Phase != daemonapi.ResourcePhaseBound || resource.Observed["leaseState"] != string(dhcpv4client.StateRenewing) {
		t.Fatalf("renewal withdrew usable authority: %+v", resource)
	}
	d.mu.Lock()
	*now = d.snapshotLocked().ExpiresAt
	d.mu.Unlock()
	response = httptest.NewRecorder()
	d.handleStatus(response, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if resource := status.Resources[0]; resource.Phase != daemonapi.ResourcePhaseExpired || resource.Observed["currentAddress"] != "" {
		t.Fatalf("expired status retained authority: %+v", resource)
	}
	conn.request(t) // the actual receive/timer loop starts a fresh discovery
	d.mu.Lock()     // wait for its send/persistence operation to finish
	d.mu.Unlock()
	data, err := os.ReadFile(d.opts.leaseFile)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot dhcpv4client.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.CurrentAddress != "" {
		t.Fatalf("persisted expired address: %+v", snapshot)
	}
}
func TestBroadcastFrameUsesProtocolSource(t *testing.T) {
	packet := dhcpv4client.Packet{Payload: []byte{1, 2, 3}, Source: netip.IPv4Unspecified(), Destination: netip.MustParseAddr("255.255.255.255")}
	frame := ethernetBroadcast(net.HardwareAddr{2, 0, 0, 0, 0, 1}, packet)
	if !net.IP(frame[26:30]).IsUnspecified() || !net.IP(frame[30:34]).Equal(net.IPv4bcast) || ipv4Checksum(frame[14:34]) != 0 {
		t.Fatal("invalid initial DHCP source/header")
	}
	packet.Source = netip.MustParseAddr("192.0.2.10")
	frame = ethernetBroadcast(net.HardwareAddr{2, 0, 0, 0, 0, 1}, packet)
	if net.IP(frame[26:30]).String() != "192.0.2.10" {
		t.Fatal("rebind did not retain leased IP source")
	}
}

func TestDaemonTimersRenewAndRebindWithinOneSecondLease(t *testing.T) {
	d, conn, _ := testDaemon(t)
	client, err := dhcpv4client.New(dhcpv4client.Config{Resource: "wan", Interface: "eth0", HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 255}}, conn)
	if err != nil {
		t.Fatal(err)
	}
	d.client = client
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.loop(ctx, false) }()
	defer func() {
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Errorf("worker stopped: %v", err)
		}
	}()
	discover := conn.request(t)
	conn.receive <- daemonReply(t, discover, dhcpv4client.MessageOffer)
	request := conn.request(t)
	ack, err := dhcpv4client.Decode(daemonReply(t, request, dhcpv4client.MessageACK))
	if err != nil {
		t.Fatal(err)
	}
	ack.Options[dhcpv4client.OptionLeaseTime] = []byte{0, 0, 0, 1}
	payload, err := dhcpv4client.Encode(ack)
	if err != nil {
		t.Fatal(err)
	}
	conn.receive <- payload
	for _, destination := range []string{"192.0.2.1", "255.255.255.255"} {
		select {
		case packet := <-conn.sent:
			msg, err := dhcpv4client.Decode(packet.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if msg.MessageType() != dhcpv4client.MessageRequest || msg.CIAddr.String() != "192.0.2.10" || packet.Destination.String() != destination {
				t.Fatalf("short lease skipped timer: %+v %+v", msg, packet)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no request to %s", destination)
		}
	}
	if message := conn.request(t); message.MessageType() != dhcpv4client.MessageDiscover {
		t.Fatal("one-second lease did not expire")
	}
}
