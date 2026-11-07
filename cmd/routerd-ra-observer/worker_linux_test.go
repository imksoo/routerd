// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package main

import (
	"context"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv6"
)

func TestObserverActualPipeReceiveCancelsAndJoins(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	defer write.Close()
	source, destination := net.ParseIP("fe80::2"), net.ParseIP("ff02::1")
	message := icmp.Message{Type: ipv6.ICMPTypeRouterAdvertisement, Body: &icmp.RawBody{Data: make([]byte, 12)}}
	payload, err := message.Marshal(icmp.IPv6PseudoHeader(source, destination))
	if err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, 14+40)
	copy(frame[0:6], []byte{0x33, 0x33, 0, 0, 0, 1})
	copy(frame[6:12], []byte{2, 0, 0, 0, 0, 2})
	binary.BigEndian.PutUint16(frame[12:14], 0x86dd)
	frame[14], frame[20], frame[21] = 0x60, 58, 255
	binary.BigEndian.PutUint16(frame[18:20], uint16(len(payload)))
	copy(frame[22:38], source.To16())
	copy(frame[38:54], destination.To16())
	frame = append(frame, payload...)
	d := newDaemon(options{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.observeSocket(ctx, &packetSocket{file: read}) }()
	if _, err := write.Write(frame); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); ; {
		d.mu.Lock()
		seen := d.packetsSeen
		d.mu.Unlock()
		if seen == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("actual receiver did not decode the real pipe packet")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled read unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("actual blocked file receive did not join")
	}
}

func TestObserverRejectsNamedPipeEventOutputBeforeOpening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- appendObserverEvent(path, daemonapi.DaemonEvent{}) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("named pipe accepted as regular event output")
		}
	case <-time.After(time.Second):
		t.Fatal("event output blocked opening a named pipe")
	}
}
