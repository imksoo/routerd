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
