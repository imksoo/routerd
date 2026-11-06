// SPDX-License-Identifier: BSD-3-Clause

package eventlog

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestLogSinkActualTCPFailureRecoveryAndClose(t *testing.T) {
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserved.Addr().String()
	if err := reserved.Close(); err != nil {
		t.Fatal(err)
	}
	sink, err := NewSink(api.LogSinkSpec{Type: "syslog", Syslog: api.LogSinkSyslogSpec{Network: "tcp", Address: address, Facility: "daemon", Tag: "routerd-wire"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := sink.Close(); err != nil {
			t.Error(err)
		}
	})
	event := Event{Level: LevelInfo, Message: "wire-recovered", Router: "private-router"}
	if err := sink.Emit(event); err == nil {
		t.Fatal("refused TCP delivery was acknowledged")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	received, failed, done := make(chan string, 1), make(chan error, 1), make(chan struct{})
	go func() {
		defer close(done)
		connection, err := listener.Accept()
		if err != nil {
			failed <- err
			return
		}
		defer connection.Close()
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		line, err := bufio.NewReader(connection).ReadString('\n')
		if err != nil {
			failed <- err
			return
		}
		received <- line
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("owned TCP receiver did not stop")
		}
	})
	if err := sink.Emit(event); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-received:
		if !strings.HasPrefix(line, "<30>") || !strings.Contains(line, "routerd-wire[") || !strings.Contains(line, "info wire-recovered router=private-router") {
			t.Fatalf("independent TCP receiver got %q", line)
		}
	case err := <-failed:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("recovered log was not received")
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sink.Emit(event); err == nil {
		t.Fatal("closed sink resumed TCP delivery")
	}
}

func TestLogSinkTCPBackpressureHasDeliveryDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	sink, err := NewSink(api.LogSinkSpec{Type: "syslog", Syslog: api.LogSinkSyslogSpec{Network: "tcp", Address: listener.Addr().String()}})
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- sink.Emit(Event{Level: LevelInfo, Message: strings.Repeat("x", 16<<20)}) }()
	var peer net.Conn
	select {
	case peer = <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("sender did not connect")
	}
	// The peer accepts TCP but never reads. Closing both sockets on failure also
	// releases the old unbounded implementation without leaking the test worker.
	defer func() {
		_ = listener.Close()
		_ = peer.Close()
		_ = sink.Close()
	}()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("backpressured delivery falsely acknowledged")
		}
	case <-time.After(6 * time.Second):
		_ = listener.Close()
		_ = peer.Close()
		select {
		case <-result:
		case <-time.After(2 * time.Second):
			t.Fatal("sender did not stop after peer cleanup")
		}
		t.Fatal("TCP syslog delivery has no bounded deadline")
	}
}
