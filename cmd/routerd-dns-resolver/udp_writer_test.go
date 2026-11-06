// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

type udpReplyFunc func([]byte) (int, error)

func (f udpReplyFunc) Write(data []byte) (int, error) { return f(data) }

func TestUDPReplySurvivesPreviousWriterReturnOverlap(t *testing.T) {
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	client, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	slot := make(chan struct{}, 1)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	first := dnsPacketWriter{packet: server, slot: slot, Writer: udpReplyFunc(func(data []byte) (int, error) {
		n, err := server.WriteTo(data, client.LocalAddr())
		// The kernel delivered the reply; the server goroutine has not yet
		// returned to release its shared response-write lock.
		<-release
		return n, err
	})}
	done := make(chan error, 1)
	go func() { _, err := first.Write([]byte("first")); done <- err }()
	if err := client.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	n, _, err := client.ReadFrom(buf)
	if err != nil || string(buf[:n]) != "first" {
		t.Fatalf("first UDP reply: %q %v", buf[:n], err)
	}
	second := dnsPacketWriter{packet: server, slot: slot, Writer: udpReplyFunc(func(data []byte) (int, error) { return server.WriteTo(data, client.LocalAddr()) })}
	// Model a brief scheduling gap after the first real reply. A concurrent
	// reply must use the existing write budget, not be discarded immediately.
	timer := time.AfterFunc(100*time.Millisecond, unblock)
	defer timer.Stop()
	_, secondErr := second.Write([]byte("second"))
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if secondErr != nil {
		t.Fatalf("second UDP reply was discarded: %v", secondErr)
	}
	n, _, err = client.ReadFrom(buf)
	if err != nil || string(buf[:n]) != "second" {
		t.Fatalf("second UDP reply: %q %v", buf[:n], err)
	}
}

func TestUDPReplyBusyWriterHonorsSharedBudget(t *testing.T) {
	slot := make(chan struct{}, 1)
	slot <- struct{}{}
	writer := dnsPacketWriter{slot: slot}
	started := time.Now()
	_, err := writer.Write([]byte("reply"))
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("busy writer: %v", err)
	}
	elapsed := time.Since(started)
	if elapsed < dnsResponseWriteTimeout || elapsed > dnsResponseWriteTimeout+time.Second {
		t.Fatalf("write budget elapsed %v", elapsed)
	}
	if len(slot) != 1 {
		t.Fatal("timeout released another writer's slot")
	}
}
